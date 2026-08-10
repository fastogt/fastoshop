package database

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestImages(t *testing.T) {
	d := openTest(t)
	p := &Product{Title: "x", Currency: "RUB"}
	_ = d.CreateProduct(p)
	if err := d.AddImage(p.ID, "a.jpg"); err != nil {
		t.Fatal(err)
	}
	_ = d.AddImage(p.ID, "b.jpg")
	imgs, err := d.ListImages(p.ID)
	if err != nil || len(imgs) != 2 || imgs[0].Path != "a.jpg" {
		t.Fatalf("%v %+v", err, imgs)
	}
	if err := d.DeleteImage(imgs[1].ID); err != nil {
		t.Fatal(err)
	}
}

func TestOrders(t *testing.T) {
	d := openTest(t)
	o := &Order{Name: "Иван", Phone: "+79990001122", ItemsJSON: `[{"sku":"T-1","qty":1}]`}
	if err := d.CreateOrder(o); err != nil {
		t.Fatal(err)
	}
	if err := d.SetOrderStatus(o.ID, "done"); err != nil {
		t.Fatal(err)
	}
	list, err := d.ListOrders()
	if err != nil || len(list) != 1 || list[0].Status != "done" {
		t.Fatalf("%v %+v", err, list)
	}
}

func TestSettingsAndTokens(t *testing.T) {
	d := openTest(t)
	if _, err := d.GetSettings(); err == nil {
		t.Fatal("settings must not exist yet")
	}
	s := &Settings{OwnerEmail: "a@b.c", PasswordHash: "h"}
	if err := d.CreateSettings(s); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateSettings(s); err == nil {
		t.Fatal("second owner must fail")
	}
	s.ShopName = "Лавка"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetSettings()
	if err != nil || got.ShopName != "Лавка" {
		t.Fatalf("%v %+v", err, got)
	}

	if err := d.CreateToken("tok1", "session", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !d.ValidToken("tok1", "session") {
		t.Fatal("token must be valid")
	}
	if d.ValidToken("tok1", "reset") {
		t.Fatal("wrong purpose accepted")
	}
	_ = d.CreateToken("old", "session", time.Now().Add(-time.Hour))
	if d.ValidToken("old", "session") {
		t.Fatal("expired token accepted")
	}
}

func TestResetOwnerPassword(t *testing.T) {
	d := openTest(t)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("oldpass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreateSettings(&Settings{OwnerEmail: "a@b.c", PasswordHash: string(oldHash)}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateToken("tok1", "session", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	pw, err := d.ResetOwnerPassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 16 {
		t.Fatalf("expected 16-char password, got %q", pw)
	}

	s, err := d.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte(pw)) != nil {
		t.Fatal("new password does not validate against stored hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte("oldpass123")) == nil {
		t.Fatal("old password still validates")
	}
	if d.ValidToken("tok1", "session") {
		t.Fatal("old session must be invalidated")
	}
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM auth_tokens`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("auth_tokens must be empty after reset")
	}
}

func TestCreateOwner(t *testing.T) {
	d := openTest(t)
	pw, err := d.CreateOwner("owner@shop.ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != kGeneratedPasswordLength {
		t.Fatalf("expected %d-char password, got %q", kGeneratedPasswordLength, pw)
	}
	s, err := d.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.OwnerEmail != "owner@shop.ru" {
		t.Fatalf("owner email %q", s.OwnerEmail)
	}
	if bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte(pw)) != nil {
		t.Fatal("password does not validate against stored hash")
	}
	if _, err := d.CreateOwner("attacker@evil.ru"); err == nil {
		t.Fatal("second owner must be refused")
	}
	s, err = d.GetSettings()
	if err != nil || s.OwnerEmail != "owner@shop.ru" {
		t.Fatalf("owner overwritten: %v %+v", err, s)
	}
}

func TestCleanupExpiredTokens(t *testing.T) {
	d := openTest(t)
	if err := d.CreateToken("expired", "session", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateToken("live", "session", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.CleanupExpiredTokens(); err != nil {
		t.Fatal(err)
	}
	if d.ValidToken("expired", "session") {
		t.Fatal("expired token still valid")
	}
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE token='expired'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("expired token row not deleted")
	}
	if !d.ValidToken("live", "session") {
		t.Fatal("live token must survive cleanup")
	}
}
