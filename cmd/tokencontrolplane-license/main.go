// Command tokencontrolplane-license signs offline RS256 license JWTs.
// The private key must never be committed — pass via --private-key or
// TOKENCONTROLPLANE_LICENSE_PRIVATE.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
)

func main() {
	privPath := flag.String("private-key", "", "path to RSA private key PEM (or TOKENCONTROLPLANE_LICENSE_PRIVATE)")
	inPath := flag.String("in", "", "path to license claims JSON (stdin if empty)")
	days := flag.Int("days", 365, "validity period in days")
	flag.Parse()

	path := *privPath
	if path == "" {
		path = os.Getenv("TOKENCONTROLPLANE_LICENSE_PRIVATE")
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "usage: tokencontrolplane-license --private-key key.pem --in claims.json")
		fmt.Fprintln(os.Stderr, "claims.json example:")
		fmt.Fprintln(os.Stderr, `  {"sub":"TokenControlPlane","plan":"pro","max_seats":10,"max_servers":10,"max_keys":20}`)
		fmt.Fprintln(os.Stderr, `  {"sub":"Acme","plan":"team","max_seats":25,"max_servers":25,"max_keys":50}`)
		os.Exit(2)
	}
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	priv, err := license.ParseRSAPrivateKey(pemBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse private key:", err)
		os.Exit(1)
	}

	var raw struct {
		Sub        string `json:"sub"`
		Plan       string `json:"plan"`
		MaxSeats   int    `json:"max_seats"`
		MaxServers int    `json:"max_servers"`
		MaxKeys    int    `json:"max_keys"`
	}
	var data []byte
	if *inPath == "" {
		data, err = os.ReadFile("/dev/stdin")
	} else {
		data, err = os.ReadFile(*inPath)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "read claims:", err)
		os.Exit(1)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		fmt.Fprintln(os.Stderr, "json:", err)
		os.Exit(1)
	}
	if raw.Sub == "" || raw.Plan == "" {
		fmt.Fprintln(os.Stderr, "sub and plan required")
		os.Exit(1)
	}
	now := time.Now().UTC()
	tok, err := license.Sign(priv, license.Claims{
		Plan:       raw.Plan,
		MaxSeats:   raw.MaxSeats,
		MaxServers: raw.MaxServers,
		MaxKeys:    raw.MaxKeys,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   raw.Sub,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(*days) * 24 * time.Hour)),
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
	fmt.Println(tok)
}
