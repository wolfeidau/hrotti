package store

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq" // imported for database/sql package
)

// PostgresStore postgres implementation of the user store
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore build a new user store
func NewPostgresStore(postgresURL string) *PostgresStore {

	db, err := sql.Open("postgres", postgresURL)
	if err != nil {
		log.Fatal(err)
	}
	err = db.Ping()
	if err != nil {
		log.Fatal(err)
	}
	//	defer db.Close()
	return &PostgresStore{
		db: db,
	}
}

// Health Sends a PING request to Redis.
func (s *PostgresStore) Health() bool {
	//	defer s.db.Close()
	err := s.db.Ping()
	if err != nil {
		return false
	}
	return true
}

// AuthUser validate the credentials against Postgres.
func (s *PostgresStore) AuthUser(token string) (string, error) {

	var uid string

	err := s.db.QueryRow("SELECT user_id FROM device_tokens WHERE token = $1", token).Scan(&uid)
	if err != nil {
		log.Printf("woops %s", err)
		if err == sql.ErrNoRows {
			return "", ErrUserNotFound
		}
		return "", err
	}
	return uid, nil
}

// Close the data store
func (s *PostgresStore) Close() {
	s.db.Close()
}
