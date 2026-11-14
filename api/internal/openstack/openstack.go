// Package openstack creates and removes customers' OpenStack projects and their default network.
package openstack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/projects"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/gophercloud/gophercloud/v2/pagination"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/alephc0rporation/Cardinal/api/internal/config"
)

const (
	// A client per OpenStack project holds its token and service catalog (about 10-20 KB).
	// The cache keeps the most recently used ones, and drops each one before its token's
	// hour runs out; a project that is used again logs in again.
	projectClientsMax = 1000
	projectClientsTTL = 50 * time.Minute

	customersDomain = "customers"
	// Every project gets a network, subnet and router with this name in every region.
	defaultName = "default"
	defaultCIDR = "192.168.0.0/24"
)

// Cloud is what the handlers need from OpenStack. Every method is safe to repeat.
type Cloud interface {
	// CreateProject finds or creates the Keystone project named projectID in the customers
	// domain, tagged with the tenant, and returns its ID. aleph0-api is a member of it through
	// its inherited role on the domain, so nothing is granted here.
	CreateProject(ctx context.Context, projectID, tenantID, description string) (string, error)
	// DeleteProject removes the Keystone project named projectID, found by name, so a project
	// whose ID was never saved is removed too. A missing project counts as removed.
	DeleteProject(ctx context.Context, projectID string) error
	EnsureDefaultNetwork(ctx context.Context, region, openstackProjectID string) error
	DeleteDefaultNetwork(ctx context.Context, region, openstackProjectID string) error
	CountServers(ctx context.Context, region, openstackProjectID string) (int, error)
}

type Client struct {
	cfg     config.Config
	timeout time.Duration
	regions map[string]config.Region

	mu          sync.Mutex
	provisioner *gophercloud.ProviderClient
	domainID    string
	// The cache is safe for concurrent use on its own; mu does not guard it.
	projectClients *expirable.LRU[string, *gophercloud.ProviderClient]
	extNets        map[string]string // region ID → external network ID
}

var _ Cloud = (*Client)(nil)

// New returns a client that logs in lazily, so the API starts while OpenStack is down.
// timeout bounds every HTTP call to OpenStack.
func New(cfg config.Config, timeout time.Duration) *Client {
	return newClient(cfg, timeout, projectClientsMax, projectClientsTTL)
}

func newClient(cfg config.Config, timeout time.Duration, maxProjectClients int, projectClientTTL time.Duration) *Client {
	regions := make(map[string]config.Region, len(cfg.Regions))
	for _, r := range cfg.Regions {
		regions[r.ID] = r
	}
	return &Client{
		cfg: cfg, timeout: timeout, regions: regions,
		projectClients: expirable.NewLRU[string, *gophercloud.ProviderClient](maxProjectClients, nil, projectClientTTL),
		extNets:        map[string]string{},
	}
}

// IsUnavailable reports whether err means OpenStack did not answer, as opposed to answering
// with an error.
func IsUnavailable(err error) bool {
	var ue *url.Error
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.As(err, &ue) || errors.As(err, &ne) ||
		gophercloud.ResponseCodeIs(err, http.StatusBadGateway) ||
		gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable) ||
		gophercloud.ResponseCodeIs(err, http.StatusGatewayTimeout)
}

func notFound(err error) bool { return gophercloud.ResponseCodeIs(err, http.StatusNotFound) }

// login returns a provider client that logs in again by itself when its token expires.
func (c *Client) login(ctx context.Context, user, password string, scope *gophercloud.AuthScope) (*gophercloud.ProviderClient, error) {
	p, err := openstack.NewClient(c.cfg.OSAuthURL)
	if err != nil {
		return nil, err
	}
	p.HTTPClient = http.Client{Timeout: c.timeout}
	err = openstack.Authenticate(ctx, p, gophercloud.AuthOptions{
		IdentityEndpoint: c.cfg.OSAuthURL,
		Username:         user,
		Password:         password,
		DomainID:         "default", // the users' own domain
		Scope:            scope,
		AllowReauth:      true,
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// identity returns the provisioner's Keystone client, scoped to the customers domain,
// with the domain's ID.
func (c *Client) identity(ctx context.Context) (*gophercloud.ServiceClient, string, error) {
	c.mu.Lock()
	p, domainID := c.provisioner, c.domainID
	c.mu.Unlock()
	if p == nil {
		var err error
		p, err = c.login(ctx, c.cfg.OSProvisionerUsername, c.cfg.OSProvisionerPassword,
			&gophercloud.AuthScope{DomainName: customersDomain})
		if err != nil {
			return nil, "", err
		}
		r, ok := p.GetAuthResult().(tokens.CreateResult)
		if !ok {
			return nil, "", errors.New("unexpected Keystone authentication result")
		}
		d, err := r.ExtractDomain()
		if err != nil {
			return nil, "", err
		}
		domainID = d.ID
		c.mu.Lock()
		c.provisioner, c.domainID = p, domainID
		c.mu.Unlock()
	}
	ic, err := openstack.NewIdentityV3(p, gophercloud.EndpointOpts{Availability: gophercloud.AvailabilityInternal})
	return ic, domainID, err
}

// projectClient returns aleph0-api's client scoped to one OpenStack project.
func (c *Client) projectClient(ctx context.Context, openstackProjectID string) (*gophercloud.ProviderClient, error) {
	if p, ok := c.projectClients.Get(openstackProjectID); ok {
		return p, nil
	}
	p, err := c.login(ctx, c.cfg.OSAPIUsername, c.cfg.OSAPIPassword, &gophercloud.AuthScope{ProjectID: openstackProjectID})
	if err != nil {
		return nil, err
	}
	// Two requests for the same new project may both log in; the last one is kept, both work.
	c.projectClients.Add(openstackProjectID, p)
	return p, nil
}

func (c *Client) endpoint(region string) (gophercloud.EndpointOpts, config.Region, error) {
	r, ok := c.regions[region]
	if !ok {
		return gophercloud.EndpointOpts{}, config.Region{}, fmt.Errorf("unknown region %q", region)
	}
	return gophercloud.EndpointOpts{Region: r.OpenStackRegion, Availability: gophercloud.AvailabilityInternal}, r, nil
}

func (c *Client) network(ctx context.Context, region, openstackProjectID string) (*gophercloud.ServiceClient, config.Region, error) {
	eo, r, err := c.endpoint(region)
	if err != nil {
		return nil, r, err
	}
	p, err := c.projectClient(ctx, openstackProjectID)
	if err != nil {
		return nil, r, err
	}
	nc, err := openstack.NewNetworkV2(p, eo)
	return nc, r, err
}

// first returns the ID of the first item the pager lists, or "" when it lists none.
func first[T any](ctx context.Context, pager pagination.Pager, extract func(pagination.Page) ([]T, error), id func(T) string) (string, error) {
	page, err := pager.AllPages(ctx)
	if err != nil {
		return "", err
	}
	all, err := extract(page)
	if err != nil || len(all) == 0 {
		return "", err
	}
	return id(all[0]), nil
}

func (c *Client) findProject(ctx context.Context, ic *gophercloud.ServiceClient, domainID, name string) (string, error) {
	return first(ctx, projects.List(ic, projects.ListOpts{Name: name, DomainID: domainID}), projects.ExtractProjects,
		func(p projects.Project) string { return p.ID })
}

func (c *Client) CreateProject(ctx context.Context, projectID, tenantID, description string) (string, error) {
	ic, domainID, err := c.identity(ctx)
	if err != nil {
		return "", err
	}
	osID, err := c.findProject(ctx, ic, domainID, projectID)
	if err != nil || osID != "" {
		return osID, err
	}
	p, err := projects.Create(ctx, ic, projects.CreateOpts{
		Name: projectID, DomainID: domainID, Description: description, Tags: []string{"tenant=" + tenantID},
	}).Extract()
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

func (c *Client) DeleteProject(ctx context.Context, projectID string) error {
	ic, domainID, err := c.identity(ctx)
	if err != nil {
		return err
	}
	osID, err := c.findProject(ctx, ic, domainID, projectID)
	if err != nil || osID == "" {
		return err
	}
	if err := projects.Delete(ctx, ic, osID).ExtractErr(); err != nil && !notFound(err) {
		return err
	}
	c.projectClients.Remove(osID)
	return nil
}

func (c *Client) externalNetwork(ctx context.Context, nc *gophercloud.ServiceClient, r config.Region) (string, error) {
	c.mu.Lock()
	id := c.extNets[r.ID]
	c.mu.Unlock()
	if id != "" {
		return id, nil
	}
	id, err := first(ctx, networks.List(nc, networks.ListOpts{Name: r.ExternalNetwork}), networks.ExtractNetworks,
		func(n networks.Network) string { return n.ID })
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("region %s has no network %q", r.ID, r.ExternalNetwork)
	}
	c.mu.Lock()
	c.extNets[r.ID] = id
	c.mu.Unlock()
	return id, nil
}

func (c *Client) EnsureDefaultNetwork(ctx context.Context, region, openstackProjectID string) error {
	nc, r, err := c.network(ctx, region, openstackProjectID)
	if err != nil {
		return err
	}
	extID, err := c.externalNetwork(ctx, nc, r)
	if err != nil {
		return err
	}

	netID, err := first(ctx, networks.List(nc, networks.ListOpts{Name: defaultName, ProjectID: openstackProjectID}),
		networks.ExtractNetworks, func(n networks.Network) string { return n.ID })
	if err != nil {
		return err
	}
	if netID == "" {
		n, err := networks.Create(ctx, nc, networks.CreateOpts{Name: defaultName}).Extract()
		if err != nil {
			return err
		}
		netID = n.ID
	}

	subnetID, err := first(ctx, subnets.List(nc, subnets.ListOpts{Name: defaultName, NetworkID: netID}),
		subnets.ExtractSubnets, func(s subnets.Subnet) string { return s.ID })
	if err != nil {
		return err
	}
	if subnetID == "" {
		s, err := subnets.Create(ctx, nc, subnets.CreateOpts{
			NetworkID: netID, Name: defaultName, CIDR: defaultCIDR, IPVersion: gophercloud.IPv4,
		}).Extract()
		if err != nil {
			return err
		}
		subnetID = s.ID
	}

	routerID, err := first(ctx, routers.List(nc, routers.ListOpts{Name: defaultName, ProjectID: openstackProjectID}),
		routers.ExtractRouters, func(r routers.Router) string { return r.ID })
	if err != nil {
		return err
	}
	if routerID == "" {
		rt, err := routers.Create(ctx, nc, routers.CreateOpts{
			Name: defaultName, GatewayInfo: &routers.GatewayInfo{NetworkID: extID},
		}).Extract()
		if err != nil {
			return err
		}
		routerID = rt.ID
	}

	portID, err := first(ctx, ports.List(nc, ports.ListOpts{DeviceID: routerID, NetworkID: netID}),
		ports.ExtractPorts, func(p ports.Port) string { return p.ID })
	if err != nil || portID != "" {
		return err
	}
	_, err = routers.AddInterface(ctx, nc, routerID, routers.AddInterfaceOpts{SubnetID: subnetID}).Extract()
	return err
}

func (c *Client) DeleteDefaultNetwork(ctx context.Context, region, openstackProjectID string) error {
	nc, _, err := c.network(ctx, region, openstackProjectID)
	if err != nil {
		return err
	}
	netID, err := first(ctx, networks.List(nc, networks.ListOpts{Name: defaultName, ProjectID: openstackProjectID}),
		networks.ExtractNetworks, func(n networks.Network) string { return n.ID })
	if err != nil {
		return err
	}
	routerID, err := first(ctx, routers.List(nc, routers.ListOpts{Name: defaultName, ProjectID: openstackProjectID}),
		routers.ExtractRouters, func(r routers.Router) string { return r.ID })
	if err != nil {
		return err
	}
	if routerID != "" {
		if netID != "" {
			subnetID, err := first(ctx, subnets.List(nc, subnets.ListOpts{Name: defaultName, NetworkID: netID}),
				subnets.ExtractSubnets, func(s subnets.Subnet) string { return s.ID })
			if err != nil {
				return err
			}
			if subnetID != "" {
				_, err := routers.RemoveInterface(ctx, nc, routerID, routers.RemoveInterfaceOpts{SubnetID: subnetID}).Extract()
				if err != nil && !notFound(err) {
					return err
				}
			}
		}
		if err := routers.Delete(ctx, nc, routerID).ExtractErr(); err != nil && !notFound(err) {
			return err
		}
	}
	if netID != "" {
		// Deleting the network deletes its subnet.
		if err := networks.Delete(ctx, nc, netID).ExtractErr(); err != nil && !notFound(err) {
			return err
		}
	}
	return nil
}

func (c *Client) CountServers(ctx context.Context, region, openstackProjectID string) (int, error) {
	eo, _, err := c.endpoint(region)
	if err != nil {
		return 0, err
	}
	p, err := c.projectClient(ctx, openstackProjectID)
	if err != nil {
		return 0, err
	}
	cc, err := openstack.NewComputeV2(p, eo)
	if err != nil {
		return 0, err
	}
	page, err := servers.List(cc, servers.ListOpts{}).AllPages(ctx)
	if err != nil {
		return 0, err
	}
	all, err := servers.ExtractServers(page)
	return len(all), err
}
