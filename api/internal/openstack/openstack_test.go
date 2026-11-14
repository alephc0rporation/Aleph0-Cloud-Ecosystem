package openstack_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"

	"github.com/alephc0rporation/Cardinal/api/internal/config"
	"github.com/alephc0rporation/Cardinal/api/internal/openstack"
)

func testConfig(authURL string) config.Config {
	return config.Config{
		OSAuthURL:     authURL,
		OSAPIUsername: "aleph0-api", OSAPIPassword: "x",
		OSProvisionerUsername: "aleph0-provisioner", OSProvisionerPassword: "y",
		Regions: []config.Region{{ID: "ro-1", Name: "Romania 1", OpenStackRegion: "RegionOne", ExternalNetwork: "public"}},
	}
}

// A fake Keystone and Nova: Nova rejects the first token as expired, as a real one does after an hour.
func TestLogsInAgainWhenTheTokenExpires(t *testing.T) {
	var issued, rejected atomic.Int32
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		n := issued.Add(1)
		w.Header().Set("X-Subject-Token", fmt.Sprintf("token-%d", n))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"token":{"expires_at":"2099-01-01T00:00:00.000000Z",
			"user":{"id":"u-api","name":"aleph0-api","domain":{"id":"default","name":"Default"}},
			"project":{"id":"p1","name":"p1","domain":{"id":"d1","name":"customers"}},
			"roles":[{"id":"r-member","name":"member"}],
			"catalog":[{"type":"compute","id":"c1","name":"nova","endpoints":[
				{"id":"e1","interface":"internal","region":"RegionOne","region_id":"RegionOne","url":"%s/compute"}]}]}}`, srv.URL)
	})
	// Gophercloud reads the compute API version from the endpoint root first.
	mux.HandleFunc("GET /compute/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":{"id":"v2.1","status":"CURRENT","min_version":"2.1","version":"2.96"}}`)
	})
	mux.HandleFunc("GET /compute/servers/detail", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") == "token-1" {
			rejected.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"servers":[{"id":"s1","name":"vm1"}]}`)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	c := openstack.New(testConfig(srv.URL+"/v3"), 5*time.Second)
	n, err := c.CountServers(context.Background(), "ro-1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || issued.Load() != 2 || rejected.Load() != 1 {
		t.Fatalf("servers = %d, tokens issued = %d, rejected = %d; want 1, 2, 1", n, issued.Load(), rejected.Load())
	}
}

// fakeOpenStack is a Keystone and Nova that accept every token; logins counts the tokens issued.
func fakeOpenStack(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var logins atomic.Int32
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		logins.Add(1)
		w.Header().Set("X-Subject-Token", "token")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"token":{"expires_at":"2099-01-01T00:00:00.000000Z",
			"user":{"id":"u-api","name":"aleph0-api","domain":{"id":"default","name":"Default"}},
			"catalog":[{"type":"compute","id":"c1","name":"nova","endpoints":[
				{"id":"e1","interface":"internal","region":"RegionOne","region_id":"RegionOne","url":"%s/compute"}]}]}}`, srv.URL)
	})
	mux.HandleFunc("GET /compute/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":{"id":"v2.1","status":"CURRENT","min_version":"2.1","version":"2.96"}}`)
	})
	mux.HandleFunc("GET /compute/servers/detail", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"servers":[]}`)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &logins
}

// Many requests at once, for a few projects, share the client's caches; run with -race.
func TestConcurrentCallsShareTheClient(t *testing.T) {
	srv, _ := fakeOpenStack(t)
	c := openstack.New(testConfig(srv.URL+"/v3"), 5*time.Second)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if _, err := c.CountServers(context.Background(), "ro-1", fmt.Sprintf("p%d", i%5)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

// The project client cache stays bounded: past its size it drops the least recently used
// client, and that project logs in again when it is used.
func TestProjectClientsAreEvicted(t *testing.T) {
	srv, logins := fakeOpenStack(t)
	c := openstack.NewWithCache(testConfig(srv.URL+"/v3"), 5*time.Second, 2, time.Hour)
	use := func(project string, wantLogins int32) {
		t.Helper()
		if _, err := c.CountServers(context.Background(), "ro-1", project); err != nil {
			t.Fatal(err)
		}
		if got := logins.Load(); got != wantLogins {
			t.Fatalf("after %s: %d logins, want %d", project, got, wantLogins)
		}
	}
	use("p1", 1)
	use("p2", 2)
	use("p1", 2) // cached; p1 is now the most recently used
	use("p3", 3) // the cache is full: p2, the least recently used, is dropped
	use("p1", 3) // still cached
	use("p2", 4) // dropped earlier: logs in again
}

func TestProjectClientsExpire(t *testing.T) {
	srv, logins := fakeOpenStack(t)
	c := openstack.NewWithCache(testConfig(srv.URL+"/v3"), 5*time.Second, 10, 50*time.Millisecond)
	for range 2 {
		if _, err := c.CountServers(context.Background(), "ro-1", "p1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := logins.Load(); got != 2 {
		t.Fatalf("%d logins, want 2: the client should have expired between the calls", got)
	}
}

func TestUnavailableWhenOpenStackDoesNotAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer srv.Close()

	c := openstack.New(testConfig(srv.URL+"/v3"), 200*time.Millisecond)
	start := time.Now()
	_, err := c.CountServers(context.Background(), "ro-1", "p1")
	if !openstack.IsUnavailable(err) {
		t.Fatalf("err = %v, want unavailable", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %s, want the 200 ms timeout", d)
	}
}

func TestIsUnavailable(t *testing.T) {
	for code, want := range map[int]bool{500: false, 409: false, 502: true, 503: true, 504: true} {
		err := gophercloud.ErrUnexpectedResponseCode{Actual: code}
		if got := openstack.IsUnavailable(err); got != want {
			t.Errorf("IsUnavailable(HTTP %d) = %v, want %v", code, got, want)
		}
	}
	if !openstack.IsUnavailable(context.DeadlineExceeded) {
		t.Error("IsUnavailable(deadline exceeded) = false")
	}
}
