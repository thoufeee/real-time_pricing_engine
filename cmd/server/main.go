package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"Real_TimePricingEngine/internal/feed"
	"Real_TimePricingEngine/internal/pricing"
	"Real_TimePricingEngine/internal/server"
	"Real_TimePricingEngine/internal/storage"
	pricingws "Real_TimePricingEngine/internal/websocket"
)

func main() {
	// Try loading .env from current dir or parent dir.
	if err := godotenv.Load(".env"); err != nil {
		_ = godotenv.Load("../.env")
	}

	log.Println("Initializing real-time pricing engine...")

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	// Initialize Redis and PostgreSQL.
	store, err := storage.NewStore(
		ctx,
		os.Getenv("REDIS_ADDR"),
		os.Getenv("REDIS_PASSWORD"),
		os.Getenv("POSTGRES_DSN"),
	)
	if err != nil {
		log.Fatalf("Storage initialization failed: %v", err)
	}

	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("Storage shutdown error: %v", err)
		}
	}()

	log.Println("Redis and PostgreSQL connected successfully")

	// Initialize the pricing engine.
	engine := pricing.NewEngine()

	// Restore historical data before accepting live market ticks.
	historySince := time.Now().UTC().Add(-24 * time.Hour)

	history, err := store.LoadHistory(ctx, historySince)
	if err != nil {
		log.Fatalf("Failed to load price history: %v", err)
	}

	if err := engine.Restore(history); err != nil {
		log.Fatalf("Failed to restore pricing engine state: %v", err)
	}

	log.Printf(
		"Restored %d historical price snapshots from PostgreSQL",
		len(history),
	)

	// Initialize WebSocket hub and HTTP router.
	hub := pricingws.NewHub()
	router := server.NewRouter(hub)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Initialize the external market feed.
	client, err := feed.NewClient(feed.Config{
		URL:       os.Getenv("LIVEFXHUB_WS_URL"),
		APIKey:    os.Getenv("API_KEY"),
		SecretKey: os.Getenv("SECRET_KEY"),
	})
	if err != nil {
		log.Fatalf("Market feed initialization failed: %v", err)
	}

	// Start the market feed.
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		log.Println("Starting market feed...")

		for {
			if ctx.Err() != nil {
				return
			}

			err := client.Run(ctx, func(payload []byte) {
				updates, err := engine.Process(payload)
				if err != nil {
					log.Printf("Failed to process market feed payload: %v", err)
					return
				}

				for _, snapshot := range updates {
					if err := store.SaveSnapshot(ctx, snapshot); err != nil {
						log.Printf(
							"Failed to persist snapshot for %s: %v",
							snapshot.Symbol,
							err,
						)
						continue
					}

					// Publish the latest pricing data to subscribed clients.
					hub.Broadcast(snapshot, snapshot.Symbol)

					log.Printf(
						"%s buy=%f sell=%f high=%f low=%f change=%.4f%%",
						snapshot.Symbol,
						snapshot.Buy,
						snapshot.Sell,
						snapshot.High24h,
						snapshot.Low24h,
						snapshot.Change24h,
					)
				}
			})

			if err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("Market feed disconnected: %v. Reconnecting in 5s...", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			} else {
				return
			}
		}
	}()

	// Start the HTTP and downstream WebSocket server.
	serverErr := make(chan error, 1)

	go func() {
		log.Printf("Pricing engine listening on :%s", port)

		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}

		serverErr <- nil
	}()

	// Wait for a shutdown signal or HTTP server failure.
	select {
	case <-ctx.Done():
		log.Println("Shutdown signal received")

	case err := <-serverErr:
		if err != nil {
			log.Printf("HTTP server failed: %v", err)
		}
		cancel()
	}

	// Gracefully shut down the HTTP server.
	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
		_ = httpServer.Close()
	}

	cancel()
	wg.Wait()

	log.Println("Pricing engine stopped")
}
