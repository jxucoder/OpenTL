package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/jxucoder/telecoder/internal/config"
	"github.com/jxucoder/telecoder/internal/engine"
	"github.com/jxucoder/telecoder/internal/server"
	"github.com/jxucoder/telecoder/internal/store"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the TeleCoder server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()

		st, err := store.Open(cfg.DataDir)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer st.Close()

		eng := engine.New(cfg, st)
		srv := server.New(eng)

		log.Printf("TeleCoder listening on %s", cfg.ListenAddr)
		return http.ListenAndServe(cfg.ListenAddr, srv.Handler())
	},
}
