// Package redislib is a hypothetical external library that exposes both a
// Config struct (consumed by users) and a ConfigSchema (consumed by xconfgen
// when an application embeds this library's configuration).
package redislib

import (
	"time"

	"github.com/gopherex/xconf"
	"github.com/gopherex/xconf/pkg/validate"
)

// Config is the runtime configuration type that this library accepts.
// In a real codegen flow, this struct would be generated from ConfigSchema.
type Config struct {
	Addr     string
	Password string
	DB       int
	Timeout  time.Duration
}

// ConfigSchema is the declarative schema. Applications compose this into
// their own schema via xconf.Embed("Redis", redislib.ConfigSchema).
//
// GroupAs[Config] captures the bound Go type via the generic parameter,
// so codegen reuses redislib.Config directly instead of emitting a new
// struct.
// Env names auto-derive from the embed prefix the consumer chooses
// (e.g. Embed("Redis", ConfigSchema) yields REDIS_ADDR, REDIS_PASSWORD, ...).
var ConfigSchema = xconf.GroupAs[Config]("Config",
	xconf.String("Addr").Default("localhost:6379"),
	xconf.String("Password"),
	xconf.Int("DB").Default(0).Validate(validate.Range(0, 15)),
	xconf.Duration("Timeout").Default(5*time.Second),
)
