package session_test

import (
	"testing"

	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
)

func TestPasswordAndJWT(t *testing.T) {
	hash, err := session.HashPassword("s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if !session.CheckPassword(hash, "s3cret-pass") {
		t.Fatal("check failed")
	}
	if session.CheckPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}

	m := session.MustManager("unit-test-secret-at-least-32-chars!!")
	tok, err := m.Issue("u1", "a1", "e@x.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != "u1" || c.AccountID != "a1" || c.Role != "admin" {
		t.Fatalf("%+v", c)
	}
}
