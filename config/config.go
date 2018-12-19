package config

import (
	"fmt"
	"os"

	"github.com/naoina/toml"
)

// Config is an object created from a configuration file
// It is a list of HTTP and/or UDP relays
// Each relay has its own list of backends
type Config struct {
	HTTPRelays []HTTPConfig `toml:"http"`
	UDPRelays  []UDPConfig  `toml:"udp"`
	Verbose    bool
}

// HTTPConfig represents an HTTP relay
type HTTPConfig struct {
	// Name identifies the HTTP relay
	Name string `toml:"name"`

	// Addr should be set to the desired listening host:port
	Addr string `toml:"bind-addr"`

	// Set certificate in order to handle HTTPS requests
	SSLCombinedPem string `toml:"ssl-combined-pem"`

	// Default retention policy to set for forwarded requests
	DefaultRetentionPolicy string `toml:"default-retention-policy"`

	DefaultPingResponse int `toml:"default-ping-response"`

	// Outputs is a list of backed servers where writes will be forwarded
	Outputs []HTTPOutputConfig `toml:"output"`

	HealthTimeout int64 `toml:"health-timeout-ms"`
}

// HTTPOutputConfig represents the specification of an HTTP backend target
type HTTPOutputConfig struct {
	// Name of the backend server
	Name string `toml:"name"`

	// Location should be set to the hostname for the influxdb endpoint (for example https://influxdb.com/)
	Location string `toml:"location"`

	// Endpoints should contain the path to the different influxdb endpoints used
	Endpoints HTTPEndpointConfig `toml:"endpoints"`

	// Timeout sets a per-backend timeout for write requests (default: 10s)
	// The format used is the same seen in time.ParseDuration
	Timeout string `toml:"timeout"`

	// Buffer failed writes up to maximum count (default: 0, retry/buffering disabled)
	BufferSizeMB int `toml:"buffer-size-mb"`

	// Maximum batch size in KB (default: 512)
	MaxBatchKB int `toml:"max-batch-kb"`

	// Maximum delay between retry attempts
	// The format used is the same seen in time.ParseDuration (default: 10s)
	MaxDelayInterval string `toml:"max-delay-interval"`

	// Skip TLS verification in order to use self signed certificate
	// WARNING: It's insecure, use it only for developing and don't use in production
	SkipTLSVerification bool `toml:"skip-tls-verification"`
}

//HTTPEndpointConfig details the remote endpoints to use
type HTTPEndpointConfig struct {
	// Must be the standard write endpoint in influxdb.
	Write string `toml:"write"`
	// Must be the prometheus specific influxdb endpoint
	PromWrite string `toml:"write_prom"`
	// Must be the ping endpoint
	Ping string `toml:"ping"`
	// Must be the query influxdb endpoint
	Query string `toml:"query"`
}

// UDPConfig represents a UDP relay
type UDPConfig struct {
	// Name identifies the UDP relay
	Name string `toml:"name"`

	// Addr is where the UDP relay will listen for packets
	Addr string `toml:"bind-addr"`

	// Precision sets the precision of the timestamps (input and output)
	Precision string `toml:"precision"`

	// ReadBuffer sets the socket buffer for incoming connections
	ReadBuffer int `toml:"read-buffer"`

	// Outputs is a list of backend servers where writes will be forwarded
	Outputs []UDPOutputConfig `toml:"output"`
}

// UDPOutputConfig represents the specification of a UDP backend target
type UDPOutputConfig struct {
	// Name identifies the UDP backend
	Name string `toml:"name"`

	// Location should be set to the host:port of the backend server
	Location string `toml:"location"`

	// MTU sets the maximum output payload size, default is 1024
	MTU int `toml:"mtu"`
}

func checkDoubleSlash(endpoint HTTPEndpointConfig) HTTPEndpointConfig {
	if endpoint.PromWrite[0] == '/' {
		endpoint.PromWrite = endpoint.PromWrite[1:]
	}
	if endpoint.Ping[0] == '/' {
		endpoint.Ping = endpoint.Ping[1:]
	}
	if endpoint.Query[0] == '/' {
		endpoint.Query = endpoint.Query[1:]
	}
	if endpoint.Write[0] == '/' {
		endpoint.Write = endpoint.Write[1:]
	}
	fmt.Println(endpoint)
	return endpoint
}

// LoadConfigFile parses the specified file into a Config object
func LoadConfigFile(filename string) (Config, error) {
	var cfg Config

	f, err := os.Open(filename)
	if err != nil {
		return cfg, err
	}
	defer f.Close()

	err = toml.NewDecoder(f).Decode(&cfg)
	if err == nil {
		for i, r := range cfg.HTTPRelays {
			for j, b := range r.Outputs {
				fmt.Println("lastchar", b.Location[len(b.Location) - 1])
				if b.Location[len(b.Location) - 1] == '/' {
					cfg.HTTPRelays[i].Outputs[j].Endpoints = checkDoubleSlash(b.Endpoints)
				}
			}
		}
	}
	fmt.Printf("%+v\n", cfg)
	return cfg, err
}
