package database

import "testing"

// A false negative is the expensive case: prepared statements left on behind a
// transaction pooler collide on statement names across shared connections.
func TestIsTransactionPooler(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want bool
	}{
		{"supabase pooler port", "postgres://u.ref:pw@aws-0-eu-central-1.pooler.supabase.com:6543/postgres", true},
		{"pooler.supabase host on 5432", "postgres://u:p@aws-0.pooler.supabase.com:5432/postgres", true},
		{"pgbouncer in host", "postgres://u:p@PGBouncer.internal:5432/app", true},
		{"direct supabase connection", "postgres://postgres:p@db.ref.supabase.co:5432/postgres", false},
		{"localhost", "postgres://postgres:postgres@localhost:5432/git2", false},
		{"6543 in dbname is not the port", "postgres://u:p@h:5432/db6543", false},
		{"port 65431 is not 6543", "postgres://u:p@h:65431/db", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransactionPooler(tt.dsn); got != tt.want {
				t.Errorf("isTransactionPooler(%q) = %v, want %v", tt.dsn, got, tt.want)
			}
		})
	}
}
