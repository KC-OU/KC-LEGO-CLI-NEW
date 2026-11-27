// Package clerkbridge calls Clerk's Backend API (api.clerk.com) to create a
// matching Clerk login for a Go-side Picker/Checker/Admin account that has
// opted into the "dashboard" access channel (see internal/access.User.Dashboard).
// Both the CLI (`wms access user dashboard`) and internal/dashboardapi call
// this, so there is exactly one implementation of the actual HTTP hand-off --
// same shape as the authbridge package it replaces, which called
// kc-parts-auth-node's BetterAuth provisioning endpoint before the move to
// Clerk.
//
// Provision is a one-time credential hand-off: it sets the new Clerk
// account's password to whatever plaintext temp password/PIN the Go side
// just generated, once. A later Go-side password reset does not propagate
// automatically -- the caller must also call SyncPassword (see its own
// comment); FindUser/UpdatePassword/VerifyTOTP below exist specifically to
// make that, and a shared-TOTP bridge for telnet/TUI sign-in, possible.
//
// Verified live against Clerk's Backend API (POST /v1/users) before writing
// this: email_address is required even for a username+password account on
// this instance, Clerk rejects RFC-reserved placeholder TLDs like
// "*.invalid" ("Email address must be a valid email address"), and a
// duplicate username comes back as HTTP 422 with error code
// "form_identifier_exists" -- all three behaviors are encoded below, not
// guessed from docs. FindUser/UpdatePassword/VerifyTOTP were verified live
// the same way (see their own comments) against a disposable throwaway
// user created and deleted in the same session, never a real account.
package clerkbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// apiBase is a var, not a const, so tests can point it at an httptest.Server
// instead of the real Clerk API.
var apiBase = "https://api.clerk.com/v1"

// placeholderEmailDomain backs a synthesized email for an account with none
// (mirrors kc-parts-auth-node's old `${id}@discord.placeholder.invalid`
// convention) -- but under a real subdomain of a domain we actually own,
// since Clerk's email format validator rejects the RFC-reserved ".invalid"
// TLD that convention used.
const placeholderEmailDomain = "no-email.kcparts.xyz"

// Result mirrors the shape internal/authbridge.Result used, so call sites
// barely change: Provisioned is false (not an error) when the Clerk account
// already existed under that username.
type Result struct {
	Provisioned bool
	UserID      string
	Reason      string
	// MFAPhoneSet is true when a phone number was both requested and
	// successfully registered as a verified, reserved second factor (see
	// Provision's phone parameter) -- lets a caller tell the admin whether
	// the new account can actually get past sign-in if the Clerk instance
	// has "Require multi-factor authentication" turned on.
	MFAPhoneSet bool
}

type createUserRequest struct {
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	SkipPasswordChecks bool     `json:"skip_password_checks"`
	ExternalID         string   `json:"external_id,omitempty"`
	EmailAddress       []string `json:"email_address"`
}

// createPhoneNumberRequest hits POST /v1/phone_numbers. Clerk's TOTP
// enrollment (POST .../totp) can only be created by the signed-in user
// themselves (clerk-js's user.createTOTP()) -- there is no Backend API
// equivalent, confirmed against Clerk's own OpenAPI spec, which lists only
// DELETE for .../totp. A phone number is the one second factor the Backend
// API CAN register on a user's behalf before they ever sign in (verified
// and reserved_for_second_factor both bypass the normal SMS-code round
// trip), so it's the only way to pre-provision a working second factor for
// an account created ahead of time -- e.g. so "Require MFA" can stay on
// without new accounts getting stuck on their first sign-in.
type createPhoneNumberRequest struct {
	UserID                  string `json:"user_id"`
	PhoneNumber             string `json:"phone_number"`
	Verified                bool   `json:"verified"`
	ReservedForSecondFactor bool   `json:"reserved_for_second_factor"`
}

type clerkError struct {
	Message     string `json:"message"`
	LongMessage string `json:"long_message"`
	Code        string `json:"code"`
	Meta        struct {
		ParamName string `json:"param_name"`
	} `json:"meta"`
}

type clerkErrorResponse struct {
	Errors []clerkError `json:"errors"`
}

type clerkUser struct {
	ID string `json:"id"`
}

// Configured reports whether ClerkSecretKey is set -- callers use this to
// decide whether to even offer the "also sign into the dashboard" option,
// rather than let Provision fail later.
func Configured() bool {
	return config.Get(config.ClerkSecretKey) != ""
}

// Provision asks Clerk to create (or report already-exists for) a user for
// username/email/password. email may be "" -- a placeholder is synthesized
// the same way kc-parts-auth-node used to for a social sign-in with no
// public email. phone may also be "" -- when given (E.164, e.g. "+447..."),
// it's registered as a verified, reserved second factor on the new account
// (see createPhoneNumberRequest) so the account can actually complete sign-in
// if "Require multi-factor authentication" is on; a failure there is
// reported back via Result.Reason but never fails the whole provision --
// the account/password half already succeeded by that point.
func Provision(username, email, phone, password string) (Result, error) {
	secret := config.Get(config.ClerkSecretKey)
	if secret == "" {
		return Result{}, fmt.Errorf("dashboard login provisioning is not configured (set %s)", config.ClerkSecretKey)
	}
	if email == "" {
		email = username + "@" + placeholderEmailDomain
	}

	body, err := json.Marshal(createUserRequest{
		Username:           username,
		Password:           password,
		SkipPasswordChecks: true,
		ExternalID:         username,
		EmailAddress:       []string{email},
	})
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequest(http.MethodPost, apiBase+"/users", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var out clerkUser
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return Result{}, fmt.Errorf("malformed response from Clerk: %w", err)
		}
		result := Result{Provisioned: true, UserID: out.ID}
		if phone != "" {
			if err := provisionSecondFactorPhone(secret, out.ID, phone); err != nil {
				result.Reason = "account created, but the second-factor phone number was not accepted: " + err.Error()
			} else {
				result.MFAPhoneSet = true
			}
		}
		return result, nil
	}

	var errOut clerkErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errOut); err == nil && len(errOut.Errors) > 0 {
		first := errOut.Errors[0]
		if first.Code == "form_identifier_exists" {
			// Not an error -- the account already exists, same as
			// authbridge.Provision's "Provisioned: false" case for an
			// already-provisioned username.
			return Result{Provisioned: false, Reason: first.Message}, nil
		}
		msg := first.LongMessage
		if msg == "" {
			msg = first.Message
		}
		return Result{}, fmt.Errorf("Clerk rejected the request: %s", msg)
	}
	return Result{}, fmt.Errorf("provisioning failed (%d)", resp.StatusCode)
}

// UserInfo is what FindUser/FindUserByEmail report about an
// already-provisioned Clerk account.
type UserInfo struct {
	ID string
	// TOTPEnabled mirrors Clerk's own totp_enabled field on the user
	// object: true only once the user has self-enrolled an authenticator
	// via Clerk's own UI (createTOTP()/Confirm, same as the dashboard's
	// Profile page) -- there is no Backend API way to enroll one on a
	// user's behalf, confirmed against Clerk's OpenAPI spec (see
	// Provision's own comment on createPhoneNumberRequest).
	TOTPEnabled bool
	// Username/Role mirror the same two custom session-token claims
	// auth.rs's Claims struct reads from a real Clerk JWT ("{{user.username}}",
	// "{{user.public_metadata.role}}") -- populated only by FindUserByEmail,
	// so the dashboard's grace-login path can hand back a result shaped
	// exactly like what a real Clerk-issued token would carry. FindUser
	// leaves both "" (its callers don't need them).
	Username string
	Role     string
	// ExternalID is the Go-side ID Provision/RenameUser store on the Clerk user. Unlike Username, a user cannot
	// change it from Clerk's own account screen.
	ExternalID string
}

type clerkUserFull struct {
	ID          string `json:"id"`
	TOTPEnabled bool   `json:"totp_enabled"`
}

// FindUser looks up the Clerk user provisioned for username, matched by
// external_id (Provision always sets external_id to the Go-side username).
// Returns nil, nil -- not an error -- when no such user exists: an
// un-dashboard-linked account, or one removed from Clerk independently of
// internal/dashboardlink.
//
// Verified live against the real API before writing this: the plural/array
// query form (external_id[]=...) is silently ignored by Clerk and falls
// back to listing ALL users unfiltered -- only the bare, singular
// external_id=<value> form actually filters. Using the wrong form here
// would make every caller below (SyncPassword, the telnet 2FA bridge) match
// an arbitrary other account instead of failing closed, so this is checked
// on every call via the exact query string below, not reconstructed.
func FindUser(username string) (*UserInfo, error) {
	secret := config.Get(config.ClerkSecretKey)
	if secret == "" {
		return nil, fmt.Errorf("dashboard login provisioning is not configured (set %s)", config.ClerkSecretKey)
	}
	req, err := http.NewRequest(http.MethodGet, apiBase+"/users?external_id="+url.QueryEscape(username)+"&limit=1", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("looking up the Clerk account failed (%d)", resp.StatusCode)
	}
	var out []clerkUserFull
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("malformed response from Clerk: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &UserInfo{ID: out[0].ID, TOTPEnabled: out[0].TOTPEnabled}, nil
}

// FindUserByEmail looks up the Clerk user whose email matches email, for the
// dashboard's 2FA grace-login path (see internal/dashboardapi's grace-login
// handler) -- a different lookup from FindUser (which matches external_id,
// the Go-side username): the dashboard login form only ever has the email
// the user typed, never a Go username, at this point in the flow.
//
// Unlike FindUser, this does NOT trust Clerk's own query-string filter to
// have actually filtered anything -- FindUser's own comment already found
// one Backend API list filter (external_id[]=) that's silently ignored and
// falls back to "every user, unfiltered" instead of erroring, and
// email_address's filtering behavior was not independently re-verified live
// this session. Given the stakes here (this result feeds straight into a
// password check and a 2FA-skip decision), every candidate Clerk returns is
// still checked client-side for an exact (case-insensitive) email match
// before being trusted; this returns a user only when exactly one candidate
// matches, and "not found" -- indistinguishable from "no account" -- for
// zero or more than one match, rather than ever guessing.
func FindUserByEmail(email string) (*UserInfo, error) {
	secret := config.Get(config.ClerkSecretKey)
	if secret == "" {
		return nil, fmt.Errorf("dashboard login provisioning is not configured (set %s)", config.ClerkSecretKey)
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}
	req, err := http.NewRequest(http.MethodGet, apiBase+"/users?email_address="+url.QueryEscape(email)+"&limit=500", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("looking up the Clerk account failed (%d)", resp.StatusCode)
	}
	var out []struct {
		clerkUserFull
		Username       string `json:"username"`
		ExternalID     string `json:"external_id"`
		PublicMetadata struct {
			Role string `json:"role"`
		} `json:"public_metadata"`
		EmailAddresses []struct {
			EmailAddress string `json:"email_address"`
		} `json:"email_addresses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("malformed response from Clerk: %w", err)
	}
	var match *UserInfo
	for _, u := range out {
		for _, addr := range u.EmailAddresses {
			if !strings.EqualFold(strings.TrimSpace(addr.EmailAddress), email) {
				continue
			}
			if match != nil {
				return nil, nil // a second candidate also matched -- refuse rather than guess which is right
			}
			match = &UserInfo{ID: u.ID, TOTPEnabled: u.TOTPEnabled, Username: linkedID(u.ExternalID, u.Username), Role: u.PublicMetadata.Role, ExternalID: u.ExternalID}
			break
		}
	}
	return match, nil
}

type verifyPasswordRequest struct {
	Password string `json:"password"`
}

type verifyPasswordResponse struct {
	Verified bool `json:"verified"`
}

// VerifyPassword independently re-checks clerkUserID's password against
// Clerk, standalone -- no browser session or prior sign-in attempt needed,
// same shape as VerifyTOTP. Used only by the dashboard's 2FA grace-login
// path: Clerk's own hosted sign-in flow cannot complete without a fresh
// second-factor code once MFA is enrolled (confirmed this session, a hard
// platform constraint, not a missing option), so re-verifying the password
// here -- rather than trying to coax a skip out of Clerk's own flow -- is
// what makes "password yes, 2FA no, within a short window" possible at all.
//
// Fails CLOSED on anything uncertain: Clerk's documented success shape is
// {"verified": true} on a 200, but the exact wrong-password response shape
// was not independently verified live this session (unlike VerifyTOTP's
// "totp_disabled" case, which was) -- so any non-200, any malformed body, or
// verified != true all return (false, nil), the same "rejected, not a
// transport failure" shape VerifyTOTP uses for a wrong code. A caller must
// never treat an error from here as "verified"; only an explicit (true, nil)
// counts as a real password match.
func VerifyPassword(clerkUserID, password string) (bool, error) {
	secret := config.Get(config.ClerkSecretKey)
	if secret == "" {
		return false, fmt.Errorf("dashboard login provisioning is not configured (set %s)", config.ClerkSecretKey)
	}
	body, err := json.Marshal(verifyPasswordRequest{Password: password})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequest(http.MethodPost, apiBase+"/users/"+url.PathEscape(clerkUserID)+"/verify_password", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil // fail closed: any non-200 is "not verified", not a transport error
	}
	var out verifyPasswordResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, nil // fail closed on a malformed body too
	}
	return out.Verified, nil
}

type updateUserRequest struct {
	Password           string `json:"password"`
	SkipPasswordChecks bool   `json:"skip_password_checks"`
}

// UpdatePassword sets clerkUserID's password immediately -- confirmed live
// (password_last_updated_at changes in the same response). skip_password_checks
// mirrors Provision: a short PIN must be accepted here too, since this
// exists only to keep an already-accepted Go-side password in sync, never
// to independently judge it.
func UpdatePassword(clerkUserID, password string) error {
	secret := config.Get(config.ClerkSecretKey)
	if secret == "" {
		return fmt.Errorf("dashboard login provisioning is not configured (set %s)", config.ClerkSecretKey)
	}
	body, err := json.Marshal(updateUserRequest{Password: password, SkipPasswordChecks: true})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPatch, apiBase+"/users/"+url.PathEscape(clerkUserID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var errOut clerkErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errOut); err == nil && len(errOut.Errors) > 0 {
		first := errOut.Errors[0]
		msg := first.LongMessage
		if msg == "" {
			msg = first.Message
		}
		return fmt.Errorf("Clerk rejected the password update: %s", msg)
	}
	return fmt.Errorf("password update failed (%d)", resp.StatusCode)
}

// SyncPassword keeps a dashboard-linked account's Clerk password identical
// to a Go-side reset -- call this after every CreateUnified/ResetUnified
// for a username that might have a linked dashboard login (see
// internal/dashboardlink), so "same ID and password on both" stays true
// across resets, not just at the moment the link was created. A username
// with no linked Clerk account is reported back via Result, not an error:
// the Go-side change already succeeded and must never be treated as failed
// just because there was nothing on the Clerk side to sync.
func SyncPassword(username, password string) (Result, error) {
	info, err := FindUser(username)
	if err != nil {
		return Result{}, err
	}
	if info == nil {
		return Result{Provisioned: false, Reason: "no linked dashboard account"}, nil
	}
	if err := UpdatePassword(info.ID, password); err != nil {
		return Result{}, err
	}
	return Result{Provisioned: true, UserID: info.ID}, nil
}

type verifyTOTPRequest struct {
	Code string `json:"code"`
}

// VerifyTOTP checks code against clerkUserID's own Clerk-enrolled TOTP or
// backup code, standalone -- confirmed live that this needs no browser
// session or prior sign-in attempt, just the user ID and a code.
//
// Returns (false, nil) for a wrong/rejected code: not an error, the same
// shape as "did this match", so a caller can feed it straight into its own
// lockout bookkeeping (see internal/twofa.VerifyExternal). A non-nil error
// means the request itself could not be completed -- bad user ID, Clerk
// unreachable, misconfigured secret key, or (confirmed live) the account
// has no TOTP enrolled at all ("totp_disabled") -- and must NOT be counted
// as a wrong guess by the caller. Callers should check UserInfo.TOTPEnabled
// before ever calling this, so the "totp_disabled" case shouldn't normally
// be reachable in practice; it is still handled explicitly here in case the
// user disables their Clerk TOTP in the gap between that check and this
// call.
func VerifyTOTP(clerkUserID, code string) (bool, error) {
	secret := config.Get(config.ClerkSecretKey)
	if secret == "" {
		return false, fmt.Errorf("dashboard login provisioning is not configured (set %s)", config.ClerkSecretKey)
	}
	body, err := json.Marshal(verifyTOTPRequest{Code: code})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequest(http.MethodPost, apiBase+"/users/"+url.PathEscape(clerkUserID)+"/verify_totp", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	var errOut clerkErrorResponse
	if jerr := json.NewDecoder(resp.Body).Decode(&errOut); jerr == nil && len(errOut.Errors) > 0 {
		first := errOut.Errors[0]
		if first.Code == "totp_disabled" {
			return false, fmt.Errorf("TOTP is not enrolled for this Clerk account")
		}
		// Any other 4xx (wrong code, expired, already-used backup code) is
		// a rejection, not a transport/config failure -- the same shape a
		// wrong local code already gets.
		return false, nil
	}
	return false, fmt.Errorf("verification request failed (%d)", resp.StatusCode)
}

func provisionSecondFactorPhone(secret, userID, phone string) error {
	body, err := json.Marshal(createPhoneNumberRequest{
		UserID:                  userID,
		PhoneNumber:             phone,
		Verified:                true,
		ReservedForSecondFactor: true,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, apiBase+"/phone_numbers", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var errOut clerkErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errOut); err == nil && len(errOut.Errors) > 0 {
		first := errOut.Errors[0]
		msg := first.LongMessage
		if msg == "" {
			msg = first.Message
		}
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("rejected (%d)", resp.StatusCode)
}

// clerkUserRow is the part of a Clerk user object the lookups below read.
type clerkUserRow struct {
	ID             string `json:"id"`
	TOTPEnabled    bool   `json:"totp_enabled"`
	Username       string `json:"username"`
	ExternalID     string `json:"external_id"`
	PublicMetadata struct {
		Role string `json:"role"`
	} `json:"public_metadata"`
}

func listUsers(query string) ([]clerkUserRow, error) {
	secret := config.Get(config.ClerkSecretKey)
	if secret == "" {
		return nil, fmt.Errorf("dashboard login provisioning is not configured (set %s)", config.ClerkSecretKey)
	}
	req, err := http.NewRequest(http.MethodGet, apiBase+"/users?"+query+"&limit=500", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("looking up the Clerk account failed (%d)", resp.StatusCode)
	}
	var out []clerkUserRow
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("malformed response from Clerk: %w", err)
	}
	return out, nil
}

// FindByUsername finds the one Clerk account whose username or external_id is id (case-insensitive). Like
// FindUserByEmail it never trusts Clerk's own filtering: every candidate is re-checked here, and more than one
// match is "not found" rather than a guess.
func FindByUsername(id string) (*UserInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	var match *clerkUserRow
	for _, q := range []string{"username=" + url.QueryEscape(id), "external_id=" + url.QueryEscape(id)} {
		rows, err := listUsers(q)
		if err != nil {
			return nil, err
		}
		for i, u := range rows {
			if !strings.EqualFold(u.Username, id) && !strings.EqualFold(u.ExternalID, id) {
				continue
			}
			if match != nil && match.ID != u.ID {
				return nil, nil
			}
			match = &rows[i]
		}
		if match != nil {
			break
		}
	}
	if match == nil {
		return nil, nil
	}
	return &UserInfo{ID: match.ID, TOTPEnabled: match.TOTPEnabled, Username: linkedID(match.ExternalID, match.Username), Role: match.PublicMetadata.Role, ExternalID: match.ExternalID}, nil
}

// FindByIdentifier takes what someone types to sign in: an email address or a user ID/username.
func FindByIdentifier(ident string) (*UserInfo, error) {
	if strings.Contains(ident, "@") {
		return FindUserByEmail(ident)
	}
	return FindByUsername(ident)
}

// RenameUser gives the Clerk account known as oldID (username or external_id) the new username and external_id,
// so a changed login ID keeps its dashboard link. Returns false, nil when there is no such Clerk account (an
// account that was never linked is not an error). Clerk may reject the new username (length, characters,
// already taken); that error is returned as Clerk words it.
func RenameUser(oldID, newID string) (bool, error) {
	info, err := FindByUsername(oldID)
	if err != nil || info == nil {
		return false, err
	}
	return true, PatchUser(info.ID, map[string]string{"username": newID, "external_id": newID}, "the new ID")
}

// linkedID is the ID Go uses for a Clerk account: its external_id when set, which only the backend can change, else
// its username, which the user can edit from Clerk's own account screen (so never rely on it once external_id is set).
func linkedID(externalID, username string) string {
	if strings.TrimSpace(externalID) != "" {
		return externalID
	}
	return username
}

// PatchUser updates fields on a Clerk user. what names the change in an error ("the new ID").
func PatchUser(clerkID string, fields map[string]string, what string) error {
	body, _ := json.Marshal(fields)
	req, err := http.NewRequest(http.MethodPatch, apiBase+"/users/"+url.PathEscape(clerkID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.Get(config.ClerkSecretKey))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Clerk: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var e struct {
		Errors []struct {
			LongMessage string `json:"long_message"`
		} `json:"errors"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if len(e.Errors) > 0 && e.Errors[0].LongMessage != "" {
		return fmt.Errorf("Clerk refused %s: %s", what, e.Errors[0].LongMessage)
	}
	return fmt.Errorf("Clerk refused %s (%d)", what, resp.StatusCode)
}

// LinkEmail sets the external_id of the Clerk account with this email to id: the link the dashboard should trust.
func LinkEmail(email, id string) error {
	info, err := FindUserByEmail(email)
	if err != nil {
		return err
	}
	if info == nil {
		return fmt.Errorf("no single Clerk account has that email")
	}
	return PatchUser(info.ID, map[string]string{"external_id": id}, "the link")
}
