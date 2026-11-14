package openstack_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
