package compose

import (
	"context"
	"errors"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

// --- ExternalNetworks ---

func TestExternalNetworksNone(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
`, t.TempDir())
	assert.Nil(t, p.ExternalNetworks())
}

func TestExternalNetworksTopLevel(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
networks:
  traefik:
    external: true
  internal:
    driver: bridge
`, t.TempDir())
	nets := p.ExternalNetworks()
	require.Equal(t, 1, len(nets))
	assert.Equal(t, "traefik", nets[0])
}

func TestExternalNetworksMultiple(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
networks:
  net-a:
    external: true
  net-b:
    external: true
  net-c:
    driver: bridge
`, t.TempDir())
	nets := p.ExternalNetworks()
	assert.Equal(t, 2, len(nets))
}

// --- ExternalVolumes ---

func TestExternalVolumesNone(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
`, t.TempDir())
	assert.Nil(t, p.ExternalVolumes())
}

func TestExternalVolumesTopLevel(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
volumes:
  mydata:
    external: true
  localdata: {}
`, t.TempDir())
	vols := p.ExternalVolumes()
	require.Equal(t, 1, len(vols))
	assert.Equal(t, "mydata", vols[0])
}

func TestExternalVolumesMixed(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
volumes:
  vol-a:
    external: true
  vol-b:
    external: true
  vol-c:
    driver: local
`, t.TempDir())
	vols := p.ExternalVolumes()
	assert.Equal(t, 2, len(vols))
}

// --- BindMountSources ---

func TestBindMountSourcesNone(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
`, t.TempDir())
	assert.Nil(t, p.BindMountSources())
}

func TestBindMountSourcesShortForm(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
    volumes:
      - /abs/path:/in/container
      - ./relative:/in/container
      - named-vol:/in/container
`, t.TempDir())
	srcs := p.BindMountSources()
	require.Equal(t, 1, len(srcs))
	assert.Equal(t, "/abs/path", srcs[0])
}

func TestBindMountSourcesShortFormWithOptions(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
    volumes:
      - /abs/path:/in/container:ro
`, t.TempDir())
	srcs := p.BindMountSources()
	require.Equal(t, 1, len(srcs))
	assert.Equal(t, "/abs/path", srcs[0])
}

func TestBindMountSourcesLongForm(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
    volumes:
      - type: bind
        source: /abs/path
        target: /in/container
      - type: volume
        source: named-vol
        target: /data
`, t.TempDir())
	srcs := p.BindMountSources()
	require.Equal(t, 1, len(srcs))
	assert.Equal(t, "/abs/path", srcs[0])
}

func TestBindMountSourcesDeduplicates(t *testing.T) {
	p := parseOK(t, `services:
  web:
    image: nginx
    volumes:
      - /shared:/a
  cache:
    image: redis
    volumes:
      - /shared:/b
`, t.TempDir())
	srcs := p.BindMountSources()
	assert.Equal(t, 1, len(srcs))
	assert.Equal(t, "/shared", srcs[0])
}

// --- NetworkInspect / NetworkCreate ---

func TestNetworkInspectExists(t *testing.T) {
	r := &fakeRunner{
		networkInspectOut: map[string]string{"traefik": `[{"Name":"traefik"}]`},
	}
	c := &Client{File: "f", Project: "p", r: r}
	exists, err := c.NetworkInspect(context.Background(), "traefik")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestNetworkInspectNotFound(t *testing.T) {
	r := &fakeRunner{
		networkInspectErr: map[string]error{
			"missing": errors.New("Error: No such network: missing"),
		},
	}
	c := &Client{File: "f", Project: "p", r: r}
	exists, err := c.NetworkInspect(context.Background(), "missing")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestNetworkInspectOtherError(t *testing.T) {
	r := &fakeRunner{
		networkInspectErr: map[string]error{
			"net": errors.New("daemon unreachable"),
		},
	}
	c := &Client{File: "f", Project: "p", r: r}
	_, err := c.NetworkInspect(context.Background(), "net")
	assert.NotNil(t, err)
}

func TestNetworkCreate(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	require.NoError(t, c.NetworkCreate(context.Background(), "mynet"))
	assert.Equal(t, []string{"mynet"}, r.networksCreated)
}

// --- VolumeInspect / VolumeCreate ---

func TestVolumeInspectExists(t *testing.T) {
	r := &fakeRunner{
		volumeInspectOut: map[string]string{"mydata": `[{"Name":"mydata"}]`},
	}
	c := &Client{File: "f", Project: "p", r: r}
	exists, err := c.VolumeInspect(context.Background(), "mydata")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestVolumeInspectNotFound(t *testing.T) {
	r := &fakeRunner{
		volumeInspectErr: map[string]error{
			"missing": errors.New("Error: No such volume: missing"),
		},
	}
	c := &Client{File: "f", Project: "p", r: r}
	exists, err := c.VolumeInspect(context.Background(), "missing")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestVolumeInspectOtherError(t *testing.T) {
	r := &fakeRunner{
		volumeInspectErr: map[string]error{
			"vol": errors.New("daemon unreachable"),
		},
	}
	c := &Client{File: "f", Project: "p", r: r}
	_, err := c.VolumeInspect(context.Background(), "vol")
	assert.NotNil(t, err)
}

func TestVolumeCreate(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{File: "f", Project: "p", r: r}
	require.NoError(t, c.VolumeCreate(context.Background(), "myvol"))
	assert.Equal(t, []string{"myvol"}, r.volumesCreated)
}
