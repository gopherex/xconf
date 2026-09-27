// Run from example/: go run . -once, or go run . and edit config.json.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	sp "github.com/gopherex/schemapb/go/schemapb"
	x "github.com/gopherex/xconf"
	"github.com/gopherex/xconf/contrib/sources/env"
	js "github.com/gopherex/xconf/contrib/sources/json"
)

type Config struct {
	Server struct {
		Host string `json:"host" schemapb:"default=127.0.0.1"`
		Port int64  `json:"port" schemapb:"default=8080;gte=1;lte=65535"`
	} `json:"server" schemapb:"default={}"`
}

func run() error {
	once := flag.Bool("once", false, "print initial configuration and exit")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	schema, err := sp.ReflectType[Config](sp.ID("example", "config", sp.Ver(1, 0, 0)))
	if err != nil {
		return err
	}
	runtime, err := x.OpenAs[Config](ctx, schema, js.File("config.json"), env.New(env.Prefix("APP_")))
	if err != nil {
		return err
	}
	defer runtime.Close()
	for event := range runtime.Subscribe(ctx) {
		if event.Err != nil {
			fmt.Fprintln(os.Stderr, event.Err)
			continue
		}
		cfg, err := x.Decode[Config](event.Snapshot)
		if err != nil {
			return err
		}
		fmt.Printf("version=%d server=%s:%d\n", event.Snapshot.Version(), cfg.Server.Host, cfg.Server.Port)
		if *once {
			return nil
		}
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
