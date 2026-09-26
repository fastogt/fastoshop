package database

import "testing"

// A subscription is consent, and the proof travels with it: when, from where,
// under which wording, and the token the unsubscribe link is built from.
func TestSubscribeAndUnsubscribe(t *testing.T) {
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	if err := d.Subscribe(" Buyer@Example.COM ", SubscribeFooter, "новости магазина"); err != nil {
		t.Fatal(err)
	}
	// The same address twice is a buyer pressing the button again, not an error.
	if err := d.Subscribe("buyer@example.com", SubscribeOrder, "галочка в заказе"); err != nil {
		t.Fatal(err)
	}
	if err := d.Subscribe("не почта", SubscribeFooter, ""); err == nil {
		t.Error("anything with no @ was taken for an address")
	}

	list, err := d.Subscribers()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Email != "buyer@example.com" || list[0].Token == "" {
		t.Fatalf("subscribers: %+v", list)
	}
	if list[0].ConsentText != "новости магазина" {
		t.Errorf("the wording of the first consent was overwritten: %q", list[0].ConsentText)
	}

	if active, total, _ := d.SubscriberCount(); active != 1 || total != 1 {
		t.Fatalf("count: %d of %d", active, total)
	}
	if err := d.Unsubscribe(list[0].Token); err != nil {
		t.Fatal(err)
	}
	if active, total, _ := d.SubscriberCount(); active != 0 || total != 1 {
		t.Errorf("after unsubscribing: %d active of %d", active, total)
	}
	// An unknown token says nothing about which addresses exist.
	if err := d.Unsubscribe("00000000000000000000000000000000"); err != nil {
		t.Errorf("an unknown token answered an error: %v", err)
	}
	// Subscribing again brings the address back.
	if err := d.Subscribe("buyer@example.com", SubscribeFooter, "снова"); err != nil {
		t.Fatal(err)
	}
	if active, _, _ := d.SubscriberCount(); active != 1 {
		t.Error("a returning buyer stayed unsubscribed")
	}
}
