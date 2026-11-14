package openstack

import (
	"time"

	"github.com/alephc0rporation/Cardinal/api/internal/config"
)

// NewWithCache is New with a project client cache of the given size and lifetime, for tests.
func NewWithCache(cfg config.Config, timeout time.Duration, size int, ttl time.Duration) *Client {
	return newClient(cfg, timeout, size, ttl)
}
