package clerkbridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func TestConfiguredIsFalseUntilTheSecretKeyIsSet(t *testing.T) {
	if Configured() {
		t.Fatal("expected Configured() to be false with no secret key set")
	}
	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	if !Configured() {
		t.Fatal("expected Configured() to be true once the secret key is set")
	}
}

func TestProvisionRefusesWhenNotConfigured(t *testing.T) {
	if _, err := Provision("someone", "someone@example.com", "", "pw"); err == nil {
		t.Fatal("expected an error when unconfigured")
	}
}

func TestProvisionSendsTheExpectedRequestAndParsesSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/users" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_fake" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		var body createUserRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("could not decode request body: %v", err)
		}
		if body.Username != "picker1" || body.Password != "pw123" || !body.SkipPasswordChecks {
			t.Fatalf("unexpected request body: %+v", body)
		}
		if len(body.EmailAddress) != 1 || body.EmailAddress[0] != "picker1@example.com" {
			t.Fatalf("unexpected email_address: %v", body.EmailAddress)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(clerkUser{ID: "user_abc123"})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	result, err := Provision("picker1", "picker1@example.com", "", "pw123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Provisioned || result.UserID != "user_abc123" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProvisionRegistersAPhoneNumberAsAVerifiedSecondFactor(t *testing.T) {
	var sawPhoneReq createPhoneNumberRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(clerkUser{ID: "user_phoned"})
		case "/phone_numbers":
			_ = json.NewDecoder(r.Body).Decode(&sawPhoneReq)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	result, err := Provision("picker2", "picker2@example.com", "+447700900123", "pw123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.MFAPhoneSet {
		t.Fatalf("expected MFAPhoneSet, got %+v", result)
	}
	if sawPhoneReq.UserID != "user_phoned" || sawPhoneReq.PhoneNumber != "+447700900123" || !sawPhoneReq.Verified || !sawPhoneReq.ReservedForSecondFactor {
		t.Fatalf("unexpected phone_numbers request: %+v", sawPhoneReq)
	}
}

func TestProvisionReportsButDoesNotFailOnAPhoneNumberRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(clerkUser{ID: "user_badphone"})
		case "/phone_numbers":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(clerkErrorResponse{Errors: []clerkError{{Message: "is invalid", Code: "form_param_format_invalid"}}})
		}
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	result, err := Provision("picker3", "picker3@example.com", "not-a-number", "pw123")
	if err != nil {
		t.Fatalf("expected the account creation to still succeed, got error: %v", err)
	}
	if !result.Provisioned || result.MFAPhoneSet {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Reason == "" {
		t.Fatal("expected a Reason explaining why the phone number wasn't set")
	}
}

func TestProvisionSynthesizesAPlaceholderEmailWhenNoneGiven(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createUserRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.EmailAddress) != 1 || body.EmailAddress[0] != "stockclerk2@no-email.kcparts.xyz" {
			t.Fatalf("unexpected synthesized email: %v", body.EmailAddress)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(clerkUser{ID: "user_xyz"})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	if _, err := Provision("stockclerk2", "", "", "pw123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvisionTreatsADuplicateUsernameAsNotProvisionedNotAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(clerkErrorResponse{Errors: []clerkError{{
			Message: "That username is taken. Please try another.",
			Code:    "form_identifier_exists",
		}}})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	result, err := Provision("existing", "existing@example.com", "", "pw123")
	if err != nil {
		t.Fatalf("expected no error for an already-provisioned account, got %v", err)
	}
	if result.Provisioned {
		t.Fatal("expected Provisioned to be false for an already-existing account")
	}
	if result.Reason == "" {
		t.Fatal("expected a Reason explaining the account already exists")
	}
}

func TestProvisionSurfacesAnOtherwiseUnhandledError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(clerkErrorResponse{Errors: []clerkError{{
			Message:     "is invalid",
			LongMessage: "Email address must be a valid email address.",
			Code:        "form_param_format_invalid",
		}}})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	if _, err := Provision("someone", "not-an-email", "", "pw123"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestFindUserSendsTheSingularExternalIDFormAndParsesResult(t *testing.T) {
	// Verified live before writing this: the plural/array form
	// (external_id[]=...) is silently ignored by Clerk and falls back to
	// listing every user unfiltered -- only the bare singular form filters.
	// This test pins that exact wire shape so a future refactor can't
	// accidentally regress it back to the array form.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/users" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("external_id"); got != "7354" {
			t.Fatalf("expected external_id=7354, got query %q", r.URL.RawQuery)
		}
		if strings.Contains(r.URL.RawQuery, "external_id%5B%5D") || strings.Contains(r.URL.RawQuery, "external_id[]") {
			t.Fatalf("must use the singular external_id form, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]clerkUserFull{{ID: "user_7354", TOTPEnabled: true}})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	info, err := FindUser("7354")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil || info.ID != "user_7354" || !info.TOTPEnabled {
		t.Fatalf("unexpected result: %+v", info)
	}
}

func TestFindUserReturnsNilNilWhenNoAccountIsLinked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]clerkUserFull{})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	info, err := FindUser("nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatalf("expected nil, got %+v", info)
	}
}

func TestUpdatePasswordSendsSkipPasswordChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/users/user_7354" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body updateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("could not decode request body: %v", err)
		}
		if body.Password != "654321" || !body.SkipPasswordChecks {
			t.Fatalf("unexpected request body: %+v", body)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "user_7354"})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	if err := UpdatePassword("user_7354", "654321"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncPasswordIsANoOpNotAnErrorWhenUnlinked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]clerkUserFull{})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	result, err := SyncPassword("nobody", "newpass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Provisioned {
		t.Fatal("expected Provisioned=false when there is no linked account")
	}
}

func TestSyncPasswordLooksUpThenUpdates(t *testing.T) {
	var patched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]clerkUserFull{{ID: "user_42"}})
		case http.MethodPatch:
			patched = true
			if r.URL.Path != "/users/user_42" {
				t.Fatalf("unexpected PATCH path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "user_42"})
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	result, err := SyncPassword("picker1", "newpass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Provisioned || !patched {
		t.Fatalf("expected the lookup to be followed by a PATCH, got result=%+v patched=%v", result, patched)
	}
}

func TestVerifyTOTPReturnsTrueOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/user_7354/verify_totp" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body verifyTOTPRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Code != "123456" {
			t.Fatalf("unexpected code: %q", body.Code)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"code_type": "totp", "verified": true})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	verified, err := VerifyTOTP("user_7354", "123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verified {
		t.Fatal("expected verified=true")
	}
}

// A wrong code is a rejection, not a request failure -- verified live
// (status 400, body {"errors":[...]}) against a disposable test account,
// but the live response for an ENROLLED account's wrong code specifically
// could not be captured without a browser-driven enrollment, so this test
// only pins the generic 4xx-with-errors shape, matching every other error
// response this package already handles the same way.
func TestVerifyTOTPReturnsFalseNotAnErrorOnAWrongCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(clerkErrorResponse{Errors: []clerkError{{
			Message: "is incorrect", Code: "form_code_incorrect",
		}}})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	verified, err := VerifyTOTP("user_7354", "000000")
	if err != nil {
		t.Fatalf("expected a wrong code to report (false, nil), not an error: %v", err)
	}
	if verified {
		t.Fatal("expected verified=false")
	}
}

// Confirmed live: an account with no TOTP enrolled returns this exact
// error code (400, "totp_disabled") -- this must surface as a real error,
// never as "verified=false", or a caller could mistake "nothing to check
// against" for "the code didn't match" and let a lockout counter treat it
// as a wrong guess against an account that was never actually challenged.
func TestVerifyTOTPTreatsTOTPDisabledAsAnErrorNotAWrongCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(clerkErrorResponse{Errors: []clerkError{{
			Message: "TOTP is disabled", LongMessage: "This user does not have TOTP enabled in their account",
			Code: "totp_disabled",
		}}})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	if _, err := VerifyTOTP("user_7354", "000000"); err == nil {
		t.Fatal("expected an error for totp_disabled")
	}
}

func TestFindUserByEmailMatchesExactlyOneCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/users" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "user_1", "totp_enabled": true, "username": "dave", "public_metadata": map[string]string{"role": "admin"},
				"email_addresses": []map[string]string{{"email_address": "dave@example.com"}}},
		})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	info, err := FindUserByEmail("Dave@Example.com") // case-insensitive match
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil || info.ID != "user_1" || !info.TOTPEnabled || info.Username != "dave" || info.Role != "admin" {
		t.Fatalf("unexpected result: %+v", info)
	}
}

// Pins the defensive behavior FindUserByEmail exists for: even if Clerk's
// own filter silently returns every user unfiltered (the exact failure mode
// already confirmed live for external_id[]=), this must never trust a
// candidate whose email doesn't actually match, and must refuse (not guess)
// when more than one candidate does.
func TestFindUserByEmailIgnoresNonMatchingCandidatesAndRefusesAmbiguity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "user_1", "email_addresses": []map[string]string{{"email_address": "someone-else@example.com"}}},
			{"id": "user_2", "email_addresses": []map[string]string{{"email_address": "nobody@example.com"}}},
		})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	info, err := FindUserByEmail("dave@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatalf("expected no match from an unfiltered list with no real match, got %+v", info)
	}

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "user_1", "email_addresses": []map[string]string{{"email_address": "dave@example.com"}}},
			{"id": "user_2", "email_addresses": []map[string]string{{"email_address": "dave@example.com"}}},
		})
	}))
	defer server2.Close()
	apiBase = server2.URL
	ambiguous, err := FindUserByEmail("dave@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ambiguous != nil {
		t.Fatalf("expected a refusal (nil) when two candidates match, got %+v", ambiguous)
	}
}

func TestVerifyPasswordReturnsTrueOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/user_7354/verify_password" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body verifyPasswordRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Password != "correct-horse" {
			t.Fatalf("unexpected password in request body: %q", body.Password)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(verifyPasswordResponse{Verified: true})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	ok, err := VerifyPassword("user_7354", "correct-horse")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected VerifyPassword to report true")
	}
}

func TestVerifyPasswordFailsClosedOnAWrongPasswordOrAnyNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"errors": "whatever shape Clerk actually sends"})
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	ok, err := VerifyPassword("user_7354", "wrong-password")
	if err != nil {
		t.Fatalf("a rejected password must not be a transport error, got: %v", err)
	}
	if ok {
		t.Fatal("expected VerifyPassword to report false for a non-200 response")
	}
}

func TestVerifyPasswordFailsClosedOnAMalformedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()
	apiBase = server.URL
	defer func() { apiBase = "https://api.clerk.com/v1" }()

	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	ok, err := VerifyPassword("user_7354", "whatever")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected VerifyPassword to fail closed on a malformed 200 body")
	}
}

func TestRenameUserPatchesUsernameAndExternalID(t *testing.T) {
	var patched map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users":
			// Clerk ignores a filter it does not know: return two users, only one is "checker".
			_, _ = w.Write([]byte(`[{"id":"user_other","username":"someone"},{"id":"user_chk","username":"checker","external_id":""}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/users/user_chk":
			_ = json.NewDecoder(r.Body).Decode(&patched)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	old := apiBase
	apiBase = server.URL
	defer func() { apiBase = old }()
	t.Setenv(config.ClerkSecretKey, "sk_test_fake")

	ok, err := RenameUser("checker", "7421")
	if err != nil || !ok {
		t.Fatalf("RenameUser = %v, %v", ok, err)
	}
	if patched["username"] != "7421" || patched["external_id"] != "7421" {
		t.Fatalf("patch body = %v", patched)
	}
}

func TestRenameUserWithNoClerkAccountIsNotAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer server.Close()
	old := apiBase
	apiBase = server.URL
	defer func() { apiBase = old }()
	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	if ok, err := RenameUser("nobody", "7421"); ok || err != nil {
		t.Fatalf("RenameUser = %v, %v; want false, nil", ok, err)
	}
}

func TestLinkedIDPrefersTheExternalIDUsersCannotEdit(t *testing.T) {
	if got := linkedID("7421", "7354"); got != "7421" {
		t.Fatalf("a user who renamed themselves to 7354 must still be %q, got %q", "7421", got)
	}
	if got := linkedID("", "checker"); got != "checker" {
		t.Fatalf("with no external ID the username is all there is, got %q", got)
	}
}

func TestCreateSignInTokenAsksClerkForATicketForThatUser(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sign_in_tokens" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"token":"tkt_123"}`))
	}))
	defer server.Close()
	old := apiBase
	apiBase = server.URL
	defer func() { apiBase = old }()
	t.Setenv(config.ClerkSecretKey, "sk_test_fake")
	tok, err := CreateSignInToken("user_abc")
	if err != nil || tok != "tkt_123" || got["user_id"] != "user_abc" {
		t.Fatalf("token %q err %v body %v", tok, err, got)
	}
}
