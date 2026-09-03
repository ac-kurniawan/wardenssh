// Command arca27-spike-server runs the OIDC + Casbin RBAC spike HTTP server.
//
// Configuration: config.json (checked into the spike dir) overlaid with
// environment variables — OIDC_ISSUER / OIDC_CLIENT_ID / OIDC_REDIRECT_URI are
// written by scripts/provision.sh into deploy/.env.local (source it or use a
// wrapper; see README).
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ac-kurniawan/wardenssh/spikes/arca27-oidc-casbin/internal/api"
	"github.com/ac-kurniawan/wardenssh/spikes/arca27-oidc-casbin/internal/auth"
	"github.com/ac-kurniawan/wardenssh/spikes/arca27-oidc-casbin/internal/authz"
	"github.com/ac-kurniawan/wardenssh/spikes/arca27-oidc-casbin/internal/config"
)

func main() {
	cfgPath := flag.String("config", "config.json", "path to config.json")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("config load failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr, err := auth.NewManager(
		ctx,
		cfg.OIDC.Issuer, cfg.OIDC.ClientID, cfg.OIDC.ClientSecret,
		cfg.OIDC.RedirectURI, cfg.OIDC.Scopes,
	)
	if err != nil {
		log.Error("oidc discovery failed", "issuer", cfg.OIDC.Issuer, "error", err)
		os.Exit(1)
	}
	log.Info("oidc provider discovered", "issuer", cfg.OIDC.Issuer, "client_id", cfg.OIDC.ClientID)

	en, err := authz.NewEnforcer(cfg.Authz.ModelPath, cfg.Authz.PolicyPath)
	if err != nil {
		log.Error("casbin init failed", "error", err)
		os.Exit(1)
	}
	log.Info("casbin enforcer loaded", "model", cfg.Authz.ModelPath, "policy", cfg.Authz.PolicyPath)

	srv := api.New(cfg, mgr, en, log)

	httpSrv := &http.Server{
		Addr:              ":7777",
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("spike server listening", "addr", httpSrv.Addr, "base_url", cfg.Server.BaseURL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = srv.Shutdown(shutdownCtx)
}
