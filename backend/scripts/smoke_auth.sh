#!/usr/bin/env bash
#
# Exercises every auth endpoint against a running server, plus the rules that
# are not visible from any single response: that the two login failures are
# indistinguishable, that a password change signs out other devices but not the
# caller, and that replaying a rotated refresh token revokes everything.
#
# Needs curl and jq. Writes only to accounts it creates, each stamped with the
# current time so repeated runs do not collide.
#
#   ./scripts/smoke_auth.sh [base-url]
#
set -u
API="${1:-http://localhost:8080}/api/v1"
STAMP=$(date +%s)
U1="alice+$STAMP@example.com"
U2="mallory+$STAMP@example.com"
PASS1="correct-horse-1"
PASS2="battery-staple-2"

BODY_FILE=$(mktemp)
trap 'rm -f "$BODY_FILE"' EXIT

pass=0; fail=0
req() { # method path body [token] -> sets STATUS, BODY
  local m=$1 p=$2 b=$3 t=${4:-}
  local args=(-s -o "$BODY_FILE" -w '%{http_code}' -X "$m" "$API$p" -H 'Content-Type: application/json')
  [ -n "$t" ] && args+=(-H "Authorization: Bearer $t")
  [ -n "$b" ] && args+=(-d "$b")
  STATUS=$(curl "${args[@]}")
  BODY=$(cat "$BODY_FILE")
}
check() { # label expected actual
  if [ "$2" = "$3" ]; then pass=$((pass+1)); printf '  ok   %-52s %s\n' "$1" "$3"
  else fail=$((fail+1)); printf '  FAIL %-52s expected %s got %s\n' "$1" "$2" "$3"; fi
}

echo "== signup =="
req POST /auth/signup "{\"email\":\"$U1\",\"full_name\":\"Alice Example\",\"password\":\"$PASS1\"}"
check "signup returns 201" 201 "$STATUS"
A_ACCESS=$(jq -r .access_token <<<"$BODY"); A_REFRESH=$(jq -r .refresh_token <<<"$BODY")
USER_ID=$(jq -r .user.id <<<"$BODY")
check "signup body has no password_hash" "null" "$(jq -r '.user.password_hash // "null"' <<<"$BODY")"
check "signup reports expires_in" 86400 "$(jq -r .expires_in <<<"$BODY")"
echo "  user: $(jq -c .user <<<"$BODY")"

req POST /auth/signup "{\"email\":\"$(tr '[:lower:]' '[:upper:]' <<<"$U1")\",\"full_name\":\"Alice Again\",\"password\":\"$PASS1\"}"
check "duplicate email (different case) returns 409" 409 "$STATUS"
check "  ...with code conflict" "conflict" "$(jq -r .error.code <<<"$BODY")"

req POST /auth/signup "{\"email\":\"not-an-email\",\"full_name\":\"  \",\"password\":\"short\"}"
check "invalid signup returns 400" 400 "$STATUS"
check "  ...reports all three fields" 3 "$(jq -r '.error.details | length' <<<"$BODY")"
echo "  details: $(jq -c .error.details <<<"$BODY")"

req POST /auth/signup "{\"email\":\"long+$STAMP@example.com\",\"full_name\":\"Long\",\"password\":\"$(printf 'x%.0s' {1..73})\"}"
check "password over 72 bytes returns 400" 400 "$STATUS"

echo "== login =="
req POST /auth/login "{\"email\":\"$U1\",\"password\":\"wrong-password\"}"
check "wrong password returns 401" 401 "$STATUS"
WRONG_MSG=$(jq -r .error.message <<<"$BODY")
req POST /auth/login "{\"email\":\"nobody+$STAMP@example.com\",\"password\":\"wrong-password\"}"
check "unknown email returns 401" 401 "$STATUS"
check "unknown email message identical to wrong password" "$WRONG_MSG" "$(jq -r .error.message <<<"$BODY")"

req POST /auth/login "{\"email\":\"$U1\",\"password\":\"$PASS1\"}"
check "login returns 200" 200 "$STATUS"
B_ACCESS=$(jq -r .access_token <<<"$BODY"); B_REFRESH=$(jq -r .refresh_token <<<"$BODY")

echo "== users/me =="
req GET /users/me ""
check "no token returns 401" 401 "$STATUS"
req GET /users/me "" "not-a-jwt"
check "garbage token returns 401" 401 "$STATUS"
req GET /users/me "" "$A_ACCESS"
check "valid token returns 200" 200 "$STATUS"
check "  ...returns the right user" "$USER_ID" "$(jq -r .id <<<"$BODY")"
check "  ...omits password_hash" "null" "$(jq -r '.password_hash // "null"' <<<"$BODY")"
echo "  me: $BODY"

echo "== refresh =="
req POST /auth/refresh "{\"refresh_token\":\"$A_REFRESH\"}"
check "refresh returns 200" 200 "$STATUS"
A_ACCESS2=$(jq -r .access_token <<<"$BODY"); A_REFRESH2=$(jq -r .refresh_token <<<"$BODY")
[ "$A_REFRESH" != "$A_REFRESH2" ] && check "refresh token rotated" yes yes || check "refresh token rotated" yes no
req GET /users/me "" "$A_ACCESS2"
check "new access token works" 200 "$STATUS"
req GET /users/me "" "$A_ACCESS"
check "previous access token no longer works" 401 "$STATUS"
req POST /auth/refresh "{\"refresh_token\":\"\"}"
check "empty refresh token returns 400" 400 "$STATUS"

echo "== change password =="
req PATCH /users/me/password "{\"current_password\":\"nope\",\"new_password\":\"$PASS2\"}" "$A_ACCESS2"
check "wrong current password returns 401" 401 "$STATUS"
req PATCH /users/me/password "{\"current_password\":\"$PASS1\",\"new_password\":\"$PASS1\"}" "$A_ACCESS2"
check "unchanged password returns 400" 400 "$STATUS"
req PATCH /users/me/password "{\"current_password\":\"$PASS1\",\"new_password\":\"$PASS2\"}" "$A_ACCESS2"
check "change password returns 200" 200 "$STATUS"
A_ACCESS3=$(jq -r .access_token <<<"$BODY"); A_REFRESH3=$(jq -r .refresh_token <<<"$BODY")
req GET /users/me "" "$A_ACCESS3"
check "calling session survives with new token" 200 "$STATUS"
req GET /users/me "" "$A_ACCESS2"
check "calling session's OLD token is dead" 401 "$STATUS"
req GET /users/me "" "$B_ACCESS"
check "other device signed out" 401 "$STATUS"
req POST /auth/refresh "{\"refresh_token\":\"$B_REFRESH\"}"
check "other device cannot refresh" 401 "$STATUS"
req POST /auth/login "{\"email\":\"$U1\",\"password\":\"$PASS1\"}"
check "old password rejected" 401 "$STATUS"
req POST /auth/login "{\"email\":\"$U1\",\"password\":\"$PASS2\"}"
check "new password accepted" 200 "$STATUS"
C_ACCESS=$(jq -r .access_token <<<"$BODY")

echo "== logout =="
req POST /auth/logout "" "$C_ACCESS"
check "logout returns 204" 204 "$STATUS"
req GET /users/me "" "$C_ACCESS"
check "token dead after logout" 401 "$STATUS"
req GET /users/me "" "$A_ACCESS3"
check "other session unaffected by logout" 200 "$STATUS"

echo "== refresh reuse detection =="
req POST /auth/signup "{\"email\":\"$U2\",\"full_name\":\"Mallory Example\",\"password\":\"$PASS1\"}"
M_REFRESH=$(jq -r .refresh_token <<<"$BODY")
req POST /auth/login "{\"email\":\"$U2\",\"password\":\"$PASS1\"}"
M2_ACCESS=$(jq -r .access_token <<<"$BODY")
req POST /auth/refresh "{\"refresh_token\":\"$M_REFRESH\"}"
check "legitimate rotation returns 200" 200 "$STATUS"
M_REFRESH2=$(jq -r .refresh_token <<<"$BODY"); M_ACCESS2=$(jq -r .access_token <<<"$BODY")
req POST /auth/refresh "{\"refresh_token\":\"$M_REFRESH\"}"
check "replayed refresh token returns 401" 401 "$STATUS"
req POST /auth/refresh "{\"refresh_token\":\"$M_REFRESH2\"}"
check "current refresh token also revoked" 401 "$STATUS"
req GET /users/me "" "$M_ACCESS2"
check "replay killed the replayed session" 401 "$STATUS"
req GET /users/me "" "$M2_ACCESS"
check "replay killed the user's OTHER session too" 401 "$STATUS"
req GET /users/me "" "$A_ACCESS3"
check "other user unaffected" 200 "$STATUS"

echo
echo "passed: $pass   failed: $fail"
[ "$fail" -eq 0 ]
