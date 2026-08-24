package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/machine"
	"github.com/cedar2025/xboard-node/internal/nlog"
	"github.com/cedar2025/xboard-node/internal/service"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("c", "config.yml", "config file path")
	credentialsPath := flag.String("credentials", "", "credentials.env path (defaults to the config directory)")
	showVersion := flag.Bool("v", false, "show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("xboard-node %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	if err := runHosted(*configPath, *credentialsPath); err != nil {
		reportHostError(*configPath, *credentialsPath, err)
		fmt.Fprintf(os.Stderr, "xboard-node stopped with error: %v\n", err)
		os.Exit(1)
	}
}

// runApplication owns the platform-neutral node lifecycle. Platform host
// files decide whether this lifecycle is attached to console signals or a
// Windows Service control handler.
func runApplication(ctx context.Context, configPath, credentialsPath string) error {
	return runApplicationWithReady(ctx, configPath, credentialsPath, nil)
}

// runApplicationWithReady is the service-aware variant of runApplication. The
// optional ready callback is invoked once the local startup boundary has been
// crossed: configuration and startup layout have been validated, the health
// listener is ready, and the managed runtime goroutines have been launched.
// Console callers keep using runApplication, so their lifecycle is unchanged.
func runApplicationWithReady(ctx context.Context, configPath, credentialsPath string, ready func()) error {
	if credentialsPath == "" {
		credentialsPath = filepath.Join(filepath.Dir(configPath), "credentials.env")
	}
	if err := config.LoadCredentialsFile(credentialsPath); err != nil {
		return err
	}

	rootCfg, err := config.LoadRoot(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	instances, err := rootCfg.NormalizeInstances()
	if err != nil {
		return fmt.Errorf("failed to normalize config: %w", err)
	}
	config.InitLogger(instances[0].Log)

	// Apply runtime memory tuning before anything else allocates.
	applyRuntimeConfig(instances[0].Runtime)
	return runWithReload(ctx, rootCfg, configPath, ready)
}

// runWithReload restarts all node services when the config file changes.
func runWithReload(parentCtx context.Context, initialRoot *config.RootConfig, configPath string, ready func()) error {
	var healthSrv *http.Server
	var healthPort int
	var readyOnce sync.Once
	signalReady := func() {
		if ready != nil {
			readyOnce.Do(ready)
		}
	}
	startHealth := func(port int) error {
		if port <= 0 {
			return nil
		}
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			nlog.Core().Error("failed to start health check listener", "port", port, "error", err)
			return err
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		healthSrv = srv
		healthPort = port
		go func() {
			nlog.Core().Debug(fmt.Sprintf("health check listening on :%d", port))
			if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
				nlog.Core().Warn("health check server stopped", "error", err)
			}
		}()
		return nil
	}

	initialInstances, err := initialRoot.NormalizeInstances()
	if err != nil {
		return fmt.Errorf("failed to normalize initial config: %w", err)
	}
	if err := startHealth(initialInstances[0].HealthPort); err != nil {
		return fmt.Errorf("start health check: %w", err)
	}
	defer func() {
		if healthSrv != nil {
			_ = healthSrv.Close()
		}
	}()

	for root := initialRoot; ; {
		ctx, cancel := context.WithCancel(parentCtx)

		reloadCh := make(chan *config.RootConfig, 1)

		watcher, err := config.WatchConfigRoot(ctx, configPath, func(newCfg *config.RootConfig) {
			select {
			case reloadCh <- newCfg:
			default:
			}
		})
		if err != nil {
			nlog.Core().Warn("config watcher unavailable, hot-reload disabled", "error", err)
		}

		instances, err := root.NormalizeInstances()
		if err != nil {
			cancel()
			return fmt.Errorf("failed to normalize config: %w", err)
		}
		if err := config.ValidateStartupLayout(instances); err != nil {
			cancel()
			return fmt.Errorf("startup layout validation failed: %w", err)
		}

		if instances[0].HealthPort != healthPort {
			if healthSrv != nil {
				_ = healthSrv.Close()
				healthSrv = nil
			}
			if err := startHealth(instances[0].HealthPort); err != nil {
				cancel()
				return fmt.Errorf("restart health check: %w", err)
			}
		}

		errCh := make(chan error, len(instances))
		doneCh := make(chan struct{})
		var wg sync.WaitGroup
		for _, instanceCfg := range instances {
			instanceCfg := instanceCfg
			wg.Add(1)
			go func() {
				defer wg.Done()
				if instanceCfg.IsMachineMode() {
					nlog.Core().Info("starting machine instance", "instance", instanceCfg.InstanceID, "machine_id", instanceCfg.Machine.MachineID, "panel_url", instanceCfg.Panel.URL)
					orch := machine.New(instanceCfg)
					if err := orch.Run(ctx); err != nil {
						nlog.Core().Error("machine instance exited with error", "instance", instanceCfg.InstanceID, "error", err)
						errCh <- err
						cancel()
					}
					return
				}
				nodes := instanceCfg.ExpandNodes()
				nlog.Core().Info("starting node instance", "instance", instanceCfg.InstanceID, "nodes", len(nodes), "panel_url", instanceCfg.Panel.URL)
				var instanceWG sync.WaitGroup
				for idx, nodeCfg := range nodes {
					nodeCfg := nodeCfg
					instanceWG.Add(1)
					go func(idx int) {
						defer instanceWG.Done()
						if idx > 0 {
							delay := time.Duration(idx) * 250 * time.Millisecond
							if delay > 2*time.Second {
								delay = 2 * time.Second
							}
							select {
							case <-time.After(delay):
							case <-ctx.Done():
								return
							}
						}
						svc := service.New(nodeCfg)
						if err := svc.Run(ctx); err != nil {
							nlog.Core().Error("node service exited with error", "instance", nodeCfg.InstanceID, "node_id", nodeCfg.Panel.NodeID, "error", err)
							errCh <- err
							cancel()
						}
					}(idx)
				}
				instanceWG.Wait()
			}()
		}
		go func() { wg.Wait(); close(doneCh) }()
		signalReady()

		var newRoot *config.RootConfig
		select {
		case newRoot = <-reloadCh:
			nlog.Core().Info("config changed, restarting all services...")
			cancel()
			<-doneCh
		case <-parentCtx.Done():
			nlog.Core().Info("shutdown requested, stopping services...")
			cancel()
			select {
			case <-doneCh:
			case <-time.After(15 * time.Second):
				return fmt.Errorf("shutdown timed out after 15s")
			}
		case <-doneCh:
		}

		// Always release the per-reload context, including the path where a
		// service exits on its own without calling cancel first.
		cancel()
		if watcher != nil {
			watcher.Stop()
		}

		if newRoot == nil {
			select {
			case err := <-errCh:
				if err != nil {
					return err
				}
			default:
			}
			nlog.Core().Info("stopped")
			return nil
		}

		newInstances, err := newRoot.NormalizeInstances()
		if err != nil {
			return fmt.Errorf("failed to normalize reloaded config: %w", err)
		}
		config.InitLogger(newInstances[0].Log)
		applyRuntimeConfig(newInstances[0].Runtime)
		root = newRoot
		nlog.Core().Info("reload complete, services restarting with new config")
	}
}

// applyRuntimeConfig wires up Go runtime memory limits from the config file.
// Both settings can also be overridden by environment variables (GOMEMLIMIT /
// GOGC) — the env vars take precedence because Go's runtime reads them before
// we can call these functions, but we set them here for completeness and so
// the values are logged.
func applyRuntimeConfig(rt config.RuntimeConfig) {
	// GOGC
	if rt.GoGCPercent > 0 {
		prev := debug.SetGCPercent(rt.GoGCPercent)
		nlog.Core().Info("runtime: GOGC set", "gogc", rt.GoGCPercent, "prev", prev)
	}

	// GOMEMLIMIT — parse human-readable size string (e.g. "30MiB")
	if rt.GoMemLimit != "" {
		limit, err := parseMemLimit(rt.GoMemLimit)
		if err != nil {
			nlog.Core().Warn("runtime: invalid gomemlimit, ignoring", "value", rt.GoMemLimit, "error", err)
		} else {
			prev := debug.SetMemoryLimit(limit)
			nlog.Core().Info("runtime: GOMEMLIMIT set",
				"limit", rt.GoMemLimit,
				"bytes", limit,
				"prev_bytes", prev,
			)
		}
	}
}

// parseMemLimit converts a human-readable size string to bytes.
// Supported suffixes: B, KiB, MiB, GiB, TiB (case-insensitive).
func parseMemLimit(s string) (int64, error) {
	s = strings.TrimSpace(s)
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"TiB", 1 << 40},
		{"GiB", 1 << 30},
		{"MiB", 1 << 20},
		{"KiB", 1 << 10},
		{"B", 1},
	}
	upper := strings.ToUpper(s)
	for _, sf := range suffixes {
		if strings.HasSuffix(upper, strings.ToUpper(sf.suffix)) {
			numStr := strings.TrimSuffix(upper, strings.ToUpper(sf.suffix))
			numStr = strings.TrimSpace(numStr)
			var n int64
			if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
				return 0, fmt.Errorf("parse number %q: %w", numStr, err)
			}
			return n * sf.mult, nil
		}
	}
	// No suffix: treat as raw bytes.
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("unrecognised size format %q", s)
	}
	return n, nil
}
