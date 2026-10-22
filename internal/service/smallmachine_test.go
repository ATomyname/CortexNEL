package service

import "testing"

func TestParseSmallMachineImport(t *testing.T) {
	input := "Channel: 4399mixed\n账号: demo-user\n密码: demo-pass\n{\"sauth_json\":\"{\\\"gameid\\\":\\\"x19\\\",\\\"login_channel\\\":\\\"4399com\\\"}\"}"
	got, err := ParseSmallMachineImport(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel != "4399mixed" || got.Username != "demo-user" || got.Password != "demo-pass" {
		t.Fatalf("parsed credentials = %+v", got)
	}
	if got.SAuth == "" || got.SAuth[0] != '{' {
		t.Fatalf("SAuth = %q", got.SAuth)
	}
}

func TestParseSmallMachineImportAllowsMissingSAuth(t *testing.T) {
	got, err := ParseSmallMachineImport("Channel: 4399mixed\n账号: demo\n密码: pass")
	if err != nil {
		t.Fatal(err)
	}
	if got.SAuth != "" || got.Username != "demo" || got.Password != "pass" {
		t.Fatalf("parsed credentials = %+v", got)
	}
}

func TestParseSmallMachineImportSupportsFullWidthSeparators(t *testing.T) {
	input := "Channel：4399mixed\n账号：demo\n密码：pass\n{\"sauth_json\":\"{\\\"gameid\\\":\\\"x19\\\"}\"}"
	got, err := ParseSmallMachineImport(input)
	if err != nil || got.Username != "demo" || got.Password != "pass" {
		t.Fatalf("parsed = %+v, err = %v", got, err)
	}
}
