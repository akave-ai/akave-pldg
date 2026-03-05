package config

import (
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
)

type Config struct {
	Primary       Primary              `koanf:"primary" validate:"required"`
	Server        ServerConfig         `koanf:"server" validate:"required"`
	Database      DatabaseConfig       `koanf:"database" validate:"required"`
	Observability *ObservabilityConfig `koanf:"observability" validate:"required"`
	Storage       *StorageConfig       `koanf:"storage"`       // optional; Akave O3 when set
	Ingester      *IngesterConfig      `koanf:"ingester"`     // optional; chunk flush timing
}

// IngesterConfig controls when in-memory chunks are flushed to O3.
type IngesterConfig struct {
	ChunkIdleSeconds int `koanf:"chunk_idle_seconds"` // flush after this many seconds with no new logs; 0 = use default (30 min)
	ChunkMaxEntries  int `koanf:"chunk_max_entries"` // close chunk after this many entries; 0 = use default (10000)
}

// StorageConfig holds storage backends (e.g. Akave O3).
type StorageConfig struct {
	O3 *O3Config `koanf:"o3"`
}

// O3Config is S3-compatible config for Akave O3 (https://o3-rc2.akave.xyz or similar).
type O3Config struct {
	Endpoint  string `koanf:"endpoint"`   // e.g. https://o3-rc2.akave.xyz
	Bucket    string `koanf:"bucket"`     // bucket name
	Region    string `koanf:"region"`     // e.g. us-east-1
	AccessKey string `koanf:"access_key"`
	SecretKey string `koanf:"secret_key"`
}

type Primary struct {
	Env string `koanf:"env" validate:"required"`
}

type ServerConfig struct {
	Port               string   `koanf:"port" validate:"required"`
	ReadTimeout        int      `koanf:"read_timeout" validate:"required"`
	WriteTimeout       int      `koanf:"write_timeout" validate:"required"`
	IdleTimeout        int      `koanf:"idle_timeout" validate:"required"`
	CORSAllowedOrigins []string `koanf:"cors_allowed_origins" validate:"required"`
}

type DatabaseConfig struct {
	Host            string `koanf:"host" validate:"required"`
	Port            int    `koanf:"port" validate:"required"`
	User            string `koanf:"user" validate:"required"`
	Password        string `koanf:"password"`
	Name            string `koanf:"name" validate:"required"`
	SSLMode         string `koanf:"ssl_mode" validate:"required"`
	MaxOpenConns    int    `koanf:"max_open_conns" validate:"required"`
	MaxIdleConns    int    `koanf:"max_idle_conns" validate:"required"`
	ConnMaxLifetime int    `koanf:"conn_max_lifetime" validate:"required"`
	ConnMaxIdleTime int    `koanf:"conn_max_idle_time" validate:"required"`
}

// LoadConfig loads the configuration from environment variables using koanf.
// If a .env file exists in the current directory, it is loaded first.
func LoadConfig() (mainConfig *Config, err error) {
	_ = godotenv.Load(".env") // optional; ignore if missing

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	k := koanf.New(".")
	err = k.Load(env.Provider("AKAVELOG_", ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, "AKAVELOG_"))
	}), nil)
	if err != nil {
		logger.Fatal().Err(err).Msg("could not load initial env variables")
	}

	mainConfig = &Config{}
	err = k.Unmarshal("", mainConfig)
	if err != nil {
		logger.Fatal().Err(err).Msg("could not unmarshal mainconfig")
	}

	validate := validator.New()
	err = validate.Struct(mainConfig)
	if err != nil {
		logger.Fatal().Err(err).Msg("could not validate the struct")
	}

	// set default observability config if not provided
	// in config struct we set Observability as pointer type to check whether it is nil or not
	if mainConfig.Observability == nil {
		mainConfig.Observability = DefaultObservabilityConfig()
	}

	// fill some of the fields
	mainConfig.Observability.ServiceName = "akavelog"
	mainConfig.Observability.Environment = mainConfig.Primary.Env

	// automatic pointer dereferencing for method calls
	err = mainConfig.Observability.Validate()
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid observability config")
	}

	return
}
