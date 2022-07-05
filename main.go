package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"github.com/duclmse/fengine/pkg/logger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"net/http"
	"os"
	"os/signal"
	"syscall"

	"gokit-example/account"

	migrate "github.com/rubenv/sql-migrate"
)

// const dbsource = "user=pqgotest dbname=pqgotest sslmode=verify-full"
type Config struct {
	Host        string
	Port        string
	User        string
	Pass        string
	Name        string
	// SSLMode     string
	// SSLCert     string
	// SSLKey      string
	// SSLRootCert string
}
var db *sqlx.DB 
func Connect(cfg Config, log logger.Logger) (*sqlx.DB, error) {
	
	url := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s", cfg.Host, cfg.Port, cfg.User, cfg.Name, cfg.Pass)
	log.Info("db info: %s", url)

	db, err := sqlx.Open("postgres", url)
	if err != nil {
		return nil, err
	}

	applied, err := migrateDB(db)
	if err == nil {
		log.Info("Applied %d migrations!", applied)
		return db, nil
	} else {
		log.Info("Error applying migrations: %s", err.Error())
		return nil, err
	}
}


func migrateDB(db *sqlx.DB) (int, error) {
	up := []string{
		// language=postgresql
		`DO $$BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'var_type') THEN
				CREATE TYPE VAR_TYPE AS ENUM ('i32', 'i64', 'f32', 'f64', 'bool', 'json', 'string', 'binary');
			END IF;
		END$$;`,
		// language=postgresql
		`DO $$BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'entity_type') THEN
				CREATE TYPE ENTITY_TYPE AS ENUM ('shape', 'template', 'thing');
			END IF;
		END$$;`,
		// language=postgresql
		`CREATE TABLE IF NOT EXISTS "entity" (
			"id"            UUID NOT NULL,
			"name"          VARCHAR(255) NOT NULL,
			"type"          ENTITY_TYPE  NOT NULL,
			"description"   VARCHAR(500),
			"project_id"    UUID,
			"base_template" UUID,
			"base_shapes"   UUID[],
			"create_ts" TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			"update_ts" TIMESTAMP WITHOUT TIME ZONE DEFAULT NULL,
			PRIMARY KEY (id)
		);`,
		// language=postgresql
		`CREATE TABLE IF NOT EXISTS "attribute" (
			"entity_id"    UUID NOT NULL,
			"name"         VARCHAR(255) NOT NULL,
			"type"         VAR_TYPE NOT NULL,
			"from"         UUID,
			"value_i32"    INT4,
			"value_i64"    INT4,
			"value_f32"    FLOAT4,
			"value_f64"    FLOAT8,
			"value_bool"   BOOLEAN,
			"value_json"   JSONB,
			"value_string" TEXT,
			"value_binary" BYTEA,
			"create_ts" TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			"update_ts" TIMESTAMP WITHOUT TIME ZONE DEFAULT NULL,
			PRIMARY KEY ("entity_id", "name"),
			FOREIGN KEY ("entity_id") REFERENCES entity ("id"),
			FOREIGN KEY ("from") REFERENCES entity ("id")
		);`,
		// language=postgresql
		`CREATE TABLE IF NOT EXISTS "service" (
			"entity_id" UUID NOT NULL,
			"name"      VARCHAR(255) NOT NULL,
			"input"     JSONB,
			"output"    VAR_TYPE,
			"from"      UUID,
			"code"      TEXT,
			"create_ts" TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			"update_ts" TIMESTAMP WITHOUT TIME ZONE DEFAULT NULL,
			PRIMARY KEY ("entity_id", "name"),
			FOREIGN KEY ("entity_id") REFERENCES entity ("id"),
			FOREIGN KEY ("from") REFERENCES entity ("id")
		);`,
		// language=postgresql
		`CREATE TABLE IF NOT EXISTS "subscription" (
			"entity_id" UUID NOT NULL,
			"name"      VARCHAR(255) NOT NULL,
			"subs_on"   VARCHAR(50),
			"event"     VARCHAR(50),
			"from"      UUID,
			"code"      TEXT,
			"create_ts" TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
			"update_ts" TIMESTAMP WITHOUT TIME ZONE DEFAULT NULL,
			PRIMARY KEY ("entity_id", "name"),
			FOREIGN KEY ("entity_id") REFERENCES entity ("id"),
			FOREIGN KEY ("from") REFERENCES entity ("id")
		);`,
	}
	down := []string{
		`DROP TABLE "method";`,
		`DROP TABLE "attribute";`,
		`DROP TABLE "entity";`,
		`DROP TYPE  "var_type";`,
		`DROP TYPE  "entity_type";`,
		`DROP TYPE  "method_type";`,
	}
	migrations := &migrate.MemoryMigrationSource{
		Migrations: []*migrate.Migration{
			{Id: "1", Up: up, Down: down},
		},
	}

	return migrate.Exec(db.DB, "postgres", migrations, migrate.Up)
}

func main() {
	var httpAddr = flag.String("http", ":8080", "http listen address")
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr) 
		logger = log.NewSyncLogger(logger)
		logger = log.With(logger,
			"service", "account",
			"time:", log.DefaultTimestampUTC,
			"caller", log.DefaultCaller,
		)
	}

	level.Info(logger).Log("msg", "service started")
	defer level.Info(logger).Log("msg", "service ended")

	flag.Parse()
	ctx := context.Background()
	var srv account.Service
	{
		repository := account.NewRepo(db.DB, logger)

		srv = account.NewService(repository, logger)
	}

	errs := make(chan error)

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errs <- fmt.Errorf("%s", <-c)
	}()

	endpoints := account.MakeEndpoints(srv)

	go func() {
		fmt.Println("listening on port", *httpAddr)
		handler := account.NewHTTPServer(ctx, endpoints)
		errs <- http.ListenAndServe(":8080", handler)
	}()

	level.Error(logger).Log("exit", <-errs)
}
