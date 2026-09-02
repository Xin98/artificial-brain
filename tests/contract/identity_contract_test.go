package contract

import (
	"strings"
	"testing"
)

func identityRoutes() []struct {
	path    string
	method  string
	schemas map[string]string
	body    string
} {
	return []struct {
		path    string
		method  string
		schemas map[string]string
		body    string
	}{
		{"/api/v1/auth/login/request", "post",
			map[string]string{"202": "EmptyObject", "422": "ErrorEnvelope", "429": "ErrorEnvelope", "502": "ErrorEnvelope", "503": "ErrorEnvelope"}, "LoginRequest"},
		{"/api/v1/auth/login/verify", "post",
			map[string]string{"200": "LoginResult", "401": "ErrorEnvelope", "422": "ErrorEnvelope", "429": "ErrorEnvelope"}, "LoginVerifyRequest"},
		{"/api/v1/auth/logout", "post",
			map[string]string{"200": "EmptyObject", "401": "ErrorEnvelope"}, ""},
		{"/api/v1/auth/session", "get",
			map[string]string{"200": "SessionView", "401": "ErrorEnvelope"}, ""},
		{"/api/v1/settings/contact-channels", "get",
			map[string]string{"200": "ChannelList", "401": "ErrorEnvelope"}, ""},
		{"/api/v1/settings/contact-channels", "post",
			map[string]string{"201": "ContactChannel", "401": "ErrorEnvelope", "409": "ErrorEnvelope", "422": "ErrorEnvelope", "429": "ErrorEnvelope", "502": "ErrorEnvelope", "503": "ErrorEnvelope"}, "AddChannelRequest"},
		{"/api/v1/settings/contact-channels/{channelId}/verify", "post",
			map[string]string{"200": "ChannelVerified", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "422": "ErrorEnvelope"}, "VerifyCodeRequest"},
		{"/api/v1/settings/contact-channels/{channelId}", "patch",
			map[string]string{"200": "ContactChannel", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "422": "ErrorEnvelope"}, "SetChannelEnabledRequest"},
		{"/api/v1/dev/sms-inbox", "get",
			map[string]string{"200": "DevInbox", "422": "ErrorEnvelope"}, ""},
	}
}

func TestIdentityContractRoutesCodesAndSchemas(t *testing.T) {
	document := loadDoc(t, "identity.yaml")
	if document.OpenAPI != "3.1.1" {
		t.Fatalf("openapi = %q, want 3.1.1", document.OpenAPI)
	}
	assertDocRoutes(t, document, identityRoutes())

	schemas := document.Components.Schemas
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		t.Fatalf("ErrorEnvelope = %#v", schemas["ErrorEnvelope"])
	}
	if !docClosedObject(schemas["EmptyObject"], nil) || !docPropertiesAre(schemas["EmptyObject"], nil) {
		t.Fatalf("EmptyObject = %#v", schemas["EmptyObject"])
	}

	login := schemas["LoginRequest"]
	if !docClosedObject(login, nil) ||
		!docIsString(login.Properties["phone"]) || !docMaxLength(login.Properties["phone"], 16) ||
		!docIsString(login.Properties["email"]) || !docMaxLength(login.Properties["email"], 254) {
		t.Fatalf("LoginRequest = %#v", login)
	}
	verify := schemas["LoginVerifyRequest"]
	if !docClosedObject(verify, []string{"code"}) ||
		!docIsString(verify.Properties["phone"]) || !docMaxLength(verify.Properties["phone"], 16) ||
		!docIsString(verify.Properties["email"]) || !docMaxLength(verify.Properties["email"], 254) ||
		!docMaxLength(verify.Properties["code"], 6) {
		t.Fatalf("LoginVerifyRequest = %#v", verify)
	}
	result := schemas["LoginResult"]
	if !docClosedObject(result, []string{"userId", "workspaceId", "expiresAt"}) || !docDateTime(result.Properties["expiresAt"]) {
		t.Fatalf("LoginResult = %#v", result)
	}
	session := schemas["SessionView"]
	if !docClosedObject(session, []string{"userId", "workspaceId", "sessionId"}) {
		t.Fatalf("SessionView = %#v", session)
	}

	channel := schemas["ContactChannel"]
	if !docClosedObject(channel, []string{"id", "kind", "address", "verified", "enabled", "createdAt"}) ||
		!docStringEnum(channel.Properties["kind"], []string{"email", "sms"}) ||
		!docIsBoolean(channel.Properties["verified"]) || !docIsBoolean(channel.Properties["enabled"]) ||
		!docDateTime(channel.Properties["createdAt"]) {
		t.Fatalf("ContactChannel = %#v", channel)
	}
	list := schemas["ChannelList"]
	if !docClosedObject(list, []string{"channels"}) || !docArrayOfRef(list.Properties["channels"], "ContactChannel") {
		t.Fatalf("ChannelList = %#v", list)
	}
	add := schemas["AddChannelRequest"]
	if !docClosedObject(add, []string{"kind", "address"}) || !docStringEnum(add.Properties["kind"], []string{"email", "sms"}) {
		t.Fatalf("AddChannelRequest = %#v", add)
	}
	verified := schemas["ChannelVerified"]
	if !docClosedObject(verified, []string{"verified"}) || !docIsBoolean(verified.Properties["verified"]) {
		t.Fatalf("ChannelVerified = %#v", verified)
	}
	code := schemas["VerifyCodeRequest"]
	if !docClosedObject(code, []string{"code"}) || !docMaxLength(code.Properties["code"], 6) {
		t.Fatalf("VerifyCodeRequest = %#v", code)
	}
	enabled := schemas["SetChannelEnabledRequest"]
	if !docClosedObject(enabled, []string{"enabled"}) || !docIsBoolean(enabled.Properties["enabled"]) {
		t.Fatalf("SetChannelEnabledRequest = %#v", enabled)
	}

	inbox := schemas["DevInbox"]
	if !docClosedObject(inbox, []string{"messages"}) || !docArrayOfRef(inbox.Properties["messages"], "DevInboxMessage") {
		t.Fatalf("DevInbox = %#v", inbox)
	}
	message := schemas["DevInboxMessage"]
	if !docClosedObject(message, []string{"address", "channel", "purpose", "code", "createdAt"}) ||
		!docMaxLength(message.Properties["code"], 6) || !docDateTime(message.Properties["createdAt"]) {
		t.Fatalf("DevInboxMessage = %#v", message)
	}

	// The dev inbox is double-gated and only ever appears in this contract.
	description := document.Paths["/api/v1/dev/sms-inbox"].Get.Description
	if !strings.Contains(description, "Double-gated") || !strings.Contains(description, "DEV_INBOX_ENABLED") {
		t.Fatalf("dev inbox description does not document the double gating: %q", description)
	}
}

func TestIdentityContractRejectsMutation(t *testing.T) {
	document := loadDoc(t, "identity.yaml")
	if !identityContractValid(document) {
		t.Fatal("shipped identity contract failed its own validator")
	}
	// Dropping the rate-limit response from login request must fail.
	mutated := loadDoc(t, "identity.yaml")
	request := mutated.Paths["/api/v1/auth/login/request"]
	delete(request.Post.Responses, "429")
	mutated.Paths["/api/v1/auth/login/request"] = request
	if identityContractValid(mutated) {
		t.Fatal("mutation (missing 429) unexpectedly passed validation")
	}
}

func identityContractValid(document docDocument) bool {
	if document.OpenAPI != "3.1.1" {
		return false
	}
	for _, route := range identityRoutes() {
		item, ok := document.Paths[route.path]
		if !ok {
			return false
		}
		operation := opFor(item, route.method)
		if !sameSet(mapKeys(operation.Responses), mapKeys(route.schemas)) {
			return false
		}
		for code, schema := range route.schemas {
			if operation.Responses[code].Content["application/json"].Schema.Ref != "#/components/schemas/"+schema {
				return false
			}
		}
		if route.body != "" {
			if operation.RequestBody == nil ||
				operation.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/"+route.body {
				return false
			}
		}
	}
	schemas := document.Components.Schemas
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) ||
		!docClosedObject(schemas["EmptyObject"], nil) {
		return false
	}
	login := schemas["LoginRequest"]
	if !docClosedObject(login, nil) ||
		!docIsString(login.Properties["phone"]) || !docMaxLength(login.Properties["phone"], 16) ||
		!docIsString(login.Properties["email"]) || !docMaxLength(login.Properties["email"], 254) {
		return false
	}
	verify := schemas["LoginVerifyRequest"]
	if !docClosedObject(verify, []string{"code"}) ||
		!docIsString(verify.Properties["phone"]) || !docMaxLength(verify.Properties["phone"], 16) ||
		!docIsString(verify.Properties["email"]) || !docMaxLength(verify.Properties["email"], 254) ||
		!docMaxLength(verify.Properties["code"], 6) {
		return false
	}
	if !docClosedObject(schemas["LoginResult"], []string{"userId", "workspaceId", "expiresAt"}) ||
		!docDateTime(schemas["LoginResult"].Properties["expiresAt"]) {
		return false
	}
	if !docClosedObject(schemas["SessionView"], []string{"userId", "workspaceId", "sessionId"}) {
		return false
	}
	channel := schemas["ContactChannel"]
	if !docClosedObject(channel, []string{"id", "kind", "address", "verified", "enabled", "createdAt"}) ||
		!docStringEnum(channel.Properties["kind"], []string{"email", "sms"}) {
		return false
	}
	list := schemas["ChannelList"]
	if !docClosedObject(list, []string{"channels"}) || !docArrayOfRef(list.Properties["channels"], "ContactChannel") {
		return false
	}
	add := schemas["AddChannelRequest"]
	if !docClosedObject(add, []string{"kind", "address"}) || !docStringEnum(add.Properties["kind"], []string{"email", "sms"}) {
		return false
	}
	if !docClosedObject(schemas["ChannelVerified"], []string{"verified"}) {
		return false
	}
	if !docClosedObject(schemas["VerifyCodeRequest"], []string{"code"}) || !docMaxLength(schemas["VerifyCodeRequest"].Properties["code"], 6) {
		return false
	}
	if !docClosedObject(schemas["SetChannelEnabledRequest"], []string{"enabled"}) {
		return false
	}
	inbox := schemas["DevInbox"]
	if !docClosedObject(inbox, []string{"messages"}) || !docArrayOfRef(inbox.Properties["messages"], "DevInboxMessage") {
		return false
	}
	message := schemas["DevInboxMessage"]
	return docClosedObject(message, []string{"address", "channel", "purpose", "code", "createdAt"}) &&
		docMaxLength(message.Properties["code"], 6) && docDateTime(message.Properties["createdAt"])
}
