package freqtrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config is the Binance section of the desktop Freqtrade configurator.
// DryRun means that program is in simulation and must not send exchange orders.
type Config struct {
	Found  bool
	Path   string
	Key    string
	Secret string
	DryRun bool
}

// Creds is the key pair the engine should use.
// Demo selects the Binance demo host. Paper blocks order placement.
type Creds struct {
	Key      string
	Secret   string
	Demo     bool
	Paper    bool
	FromFile bool
}

// Apply prefers the desktop configurator file. A saved local pair is used only
// when that file has no key.
func Apply(localKey, localSecret string, localDemo bool) Creds {
	file := Load()
	if file.Found && strings.TrimSpace(file.Key) != "" && strings.TrimSpace(file.Secret) != "" {
		return Creds{
			Key:      strings.TrimSpace(file.Key),
			Secret:   strings.TrimSpace(file.Secret),
			Demo:     false,
			Paper:    file.DryRun,
			FromFile: true,
		}
	}
	return Creds{
		Key:    strings.TrimSpace(localKey),
		Secret: strings.TrimSpace(localSecret),
		Demo:   localDemo,
	}
}

// Load reads freqtrade/user_data/config.json.
// DESK_FREQTRADE_CONFIG, when set, is the only path consulted.
func Load() Config {
	if p, ok := os.LookupEnv("DESK_FREQTRADE_CONFIG"); ok {
		return read(p)
	}
	for _, start := range searchRoots() {
		if p := findUp(start); p != "" {
			return read(p)
		}
	}
	return Config{}
}

func searchRoots() []string {
	var roots []string
	if wd, err := os.Getwd(); err == nil && wd != "" {
		roots = append(roots, wd)
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe))
	}
	return roots
}

func findUp(start string) string {
	dir := start
	for i := 0; i < 6; i++ {
		cand := filepath.Join(dir, "freqtrade", "user_data", "config.json")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func read(path string) Config {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}
	}
	var body struct {
		DryRun   bool `json:"dry_run"`
		Exchange struct {
			Key    string `json:"key"`
			Secret string `json:"secret"`
		} `json:"exchange"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Config{}
	}
	return Config{
		Found:  true,
		Path:   path,
		Key:    strings.TrimSpace(body.Exchange.Key),
		Secret: strings.TrimSpace(body.Exchange.Secret),
		DryRun: body.DryRun,
	}
}
