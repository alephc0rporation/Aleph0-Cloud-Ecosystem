//go:build integration

package openstack_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alephc0rporation/Cardinal/api/internal/config"
	"github.com/alephc0rporation/Cardinal/api/internal/openstack"
)

// Runs against the dev cluster with the API settings from .env (make test-integration).
func TestAgainstTheDevCluster(t *testing.T) {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Skip("needs the API settings in the environment: ", err)
	}
	c := openstack.New(cfg, 10*time.Second)
	ctx := context.Background()
	projectID, region := uuid.NewString(), cfg.Regions[0].ID

	osID, err := c.CreateProject(ctx, projectID, "integration-test", "integration test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.DeleteDefaultNetwork(context.Background(), region, osID)
		_ = c.DeleteProject(context.Background(), projectID)
	})
	again, err := c.CreateProject(ctx, projectID, "integration-test", "integration test")
	if err != nil || again != osID {
		t.Fatalf("second CreateProject = %q, %v; want the same project %q", again, err, osID)
	}

	for range 2 {
		if err := c.EnsureDefaultNetwork(ctx, region, osID); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := c.CountServers(ctx, region, osID); err != nil || n != 0 {
		t.Fatalf("CountServers = %d, %v; want 0", n, err)
	}
	for range 2 {
		if err := c.DeleteDefaultNetwork(ctx, region, osID); err != nil {
			t.Fatal(err)
		}
	}
	// By name: the OpenStack ID is not passed, as when it was never saved.
	for range 2 {
		if err := c.DeleteProject(ctx, projectID); err != nil {
			t.Fatal(err)
		}
	}
	recreated, err := c.CreateProject(ctx, projectID, "integration-test", "integration test")
	if err != nil {
		t.Fatal(err)
	}
	if recreated == osID {
		t.Fatal("DeleteProject left the Keystone project in place")
	}
	osID = recreated // the cleanup removes it
}
