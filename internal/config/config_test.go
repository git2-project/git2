package config

import "testing"

func TestExternalURL(t *testing.T) {
	cases := []struct {
		raw, env, want string
		wantErr        bool
	}{
		{raw: "", env: "dev", want: devExternalURL},
		{raw: "", env: "prod", wantErr: true},
		{raw: "https://git2.example.com", env: "prod", want: "https://git2.example.com"},
		{raw: "https://git2.example.com/", env: "prod", want: "https://git2.example.com"},
		{raw: "http://localhost:8000", env: "dev", want: "http://localhost:8000"},
		{raw: "git2.example.com", env: "dev", wantErr: true},
		{raw: "ftp://git2.example.com", env: "dev", wantErr: true},
		{raw: "https://git2.example.com/git", env: "dev", wantErr: true},
		{raw: "https://git2.example.com?x=1", env: "dev", wantErr: true},
		{raw: "https://user:pw@git2.example.com", env: "dev", wantErr: true},
	}
	for _, c := range cases {
		got, err := externalURL(c.raw, c.env)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("externalURL(%q, %q) = %q, %v; want %q, error %v", c.raw, c.env, got, err, c.want, c.wantErr)
		}
	}
}
