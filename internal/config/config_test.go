package config

import "testing"

func TestValidatePorts(t *testing.T) {
	c := Defaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Azurite.BlobPort = c.Azurite.QueuePort
	if err := c.Validate(); err == nil {
		t.Fatal("expected collision")
	}
}

func TestValidateRuntime(t *testing.T) {
	c := Defaults()
	c.Azurite.Runtime = "npx"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Azurite.Runtime = "path"
	c.Azurite.Path = ""
	if err := c.Validate(); err == nil {
		t.Fatal("expected path required")
	}
}

func TestAzuriteConnStrPorts(t *testing.T) {
	s := AzuriteConnStrPorts(20000, 20001, 20002)
	if s == AzuriteConnStr {
		t.Fatal("ports should change the string")
	}
}

func TestRedact(t *testing.T) {
	out := Redact(AzuriteConnStr)
	if out == AzuriteConnStr {
		t.Fatal("key should be masked")
	}
}

func TestIsReadOnly(t *testing.T) {
	emu := AzuriteProfile("local", 10000, 10001, 10002)
	if emu.IsReadOnly() {
		t.Fatal("emulator should be writable by default")
	}
	azure := Profile{Name: "prod", Type: ProfileAzure}
	if !azure.IsReadOnly() {
		t.Fatal("azure profile should be read-only by default")
	}
	azure.SetReadOnly(false)
	if azure.IsReadOnly() {
		t.Fatal("explicit false must win")
	}
}
