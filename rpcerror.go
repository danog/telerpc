package main

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// globalCodes are the errors any method can return, with their code, in the order of the
// PHP version's array literal. Repeated keys keep their first position and their last value,
// as in PHP (FILE_MIGRATE_%d is 400).
var globalCodes = func() *phpArray {
	a := newArray()
	for _, e := range []struct {
		err  string
		code int64
	}{
		{"FLOOD_WAIT_%d", 420},
		{"2FA_CONFIRM_WAIT_%d", 420},
		{"SLOWMODE_WAIT_%d", 420},
		{"TAKEOUT_INIT_DELAY_%d", 420},
		{"FLOOD_PREMIUM_WAIT_%d", 420},

		{"SESSION_PASSWORD_NEEDED", 401},

		{"AUTH_KEY_DUPLICATED", 406},

		{"FILE_MIGRATE_%d", 303},
		{"NETWORK_MIGRATE_%d", 303},
		{"PHONE_MIGRATE_%d", 303},
		{"STATS_MIGRATE_%d", 303},
		{"USER_MIGRATE_%d", 303},

		{"BOT_GAMES_DISABLED", 400},
		{"BOT_METHOD_INVALID", 400},
		{"CONNECTION_DEVICE_MODEL_EMPTY", 400},
		{"CONNECTION_LANG_PACK_INVALID", 400},
		{"CONNECTION_NOT_INITED", 400},
		{"CONNECTION_SYSTEM_EMPTY", 400},
		{"CONNECTION_SYSTEM_LANG_CODE_EMPTY", 400},
		{"EMAIL_UNCONFIRMED_%d", 400},
		{"FILE_MIGRATE_%d", 400},
		{"FILE_PART_%d_MISSING", 400},
		{"INPUT_CONSTRUCTOR_INVALID", 400},
		{"INPUT_FETCH_ERROR", 400},
		{"INPUT_FETCH_FAIL", 400},
		{"INPUT_LAYER_INVALID", 400},
		{"INPUT_METHOD_INVALID", 400},
		{"INPUT_REQUEST_TOO_LONG", 400},
		{"PASSWORD_TOO_FRESH_%d", 400},
		{"PEER_FLOOD", 400},
		{"POLL_VOTE_REQUIRED", 400},
		{"REPLY_MARKUP_GAME_EMPTY", 400},
		{"SESSION_TOO_FRESH_%d", 400},
		{"STICKERSET_NOT_MODIFIED", 400},
		{"TMP_PASSWORD_INVALID", 400},
		{"WEBDOCUMENT_URL_EMPTY", 400},

		{"AUTH_KEY_INVALID", 401},
		{"AUTH_KEY_PERM_EMPTY", 401},
		{"AUTH_KEY_UNREGISTERED", 401},
		{"SESSION_EXPIRED", 401},
		{"SESSION_PASSWORD_NEEDED", 401},
		{"SESSION_REVOKED", 401},
		{"USER_DEACTIVATED", 401},
		{"USER_DEACTIVATED_BAN", 401},

		{"CHAT_FORBIDDEN", 403},

		{"AUTH_KEY_DUPLICATED", 406},

		{"MSG_WAIT_TIMEOUT", -503},
		{"MSG_WAIT_FAILED", -500},
	} {
		a.Set(e.err, e.code)
	}
	return a
}()

// errorsRequireAuth can only be recorded with the auth token: anyone else gets their
// description only.
var errorsRequireAuth = []string{
	"AUTH_KEY_UNREGISTERED",
	"BOT_METHOD_INVALID",
	"USER_BOT_REQUIRED",
	"USER_BOT_INVALID",
	"BUSINESS_CONNECTION_INVALID",
}

var sanitizers = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`_XMIN$`), "_%dMIN"},
	{regexp.MustCompile(`_\d+MIN$`), "_%dMIN"},
	{regexp.MustCompile(`_X(["_])?`), "_%d${1}"},
	{regexp.MustCompile(`_\d+(["_])?`), "_%d${1}"},
	{regexp.MustCompile(`_X$`), "_%d"},
	{regexp.MustCompile(`_\d+$`), "_%d"},
}

// sanitize turns the numbers in an error (FLOOD_WAIT_5, FLOOD_WAIT_X) into %d.
func sanitize(s string) string {
	for _, r := range sanitizers {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// badErrors are the errors that are never recorded: MadelineProto's
// RPCErrorException::BAD. Keep in sync with it.
var badErrors = map[string]bool{
	"STARGIFT_RESELL_NOT_ALLOWED":       true,
	"CDN_SALTS_EMPTY":                   true,
	"BOT_POLLS_DISABLED":                true,
	"PHOTO_THUMB_URL_INVALID":           true,
	"ACTIVE_USER_REQUIRED":              true,
	"PEER_FLOOD":                        true,
	"USER_DEACTIVATED_BAN":              true,
	"INPUT_METHOD_INVALID":              true,
	"INPUT_FETCH_ERROR":                 true,
	"SESSION_REVOKED":                   true,
	"USER_DEACTIVATED":                  true,
	"RPC_SEND_FAIL":                     true,
	"RPC_CALL_FAIL":                     true,
	"RPC_MCGET_FAIL":                    true,
	"INTERDC_5_CALL_ERROR":              true,
	"INTERDC_4_CALL_ERROR":              true,
	"INTERDC_3_CALL_ERROR":              true,
	"INTERDC_2_CALL_ERROR":              true,
	"INTERDC_1_CALL_ERROR":              true,
	"INTERDC_5_CALL_RICH_ERROR":         true,
	"INTERDC_4_CALL_RICH_ERROR":         true,
	"INTERDC_3_CALL_RICH_ERROR":         true,
	"INTERDC_2_CALL_RICH_ERROR":         true,
	"INTERDC_1_CALL_RICH_ERROR":         true,
	"AUTH_KEY_DUPLICATED":               true,
	"CONNECTION_NOT_INITED":             true,
	"LOCATION_NOT_AVAILABLE":            true,
	"AUTH_KEY_INVALID":                  true,
	"LANG_CODE_EMPTY":                   true,
	"memory limit exit":                 true,
	"memory limit(?)":                   true,
	"INPUT_REQUEST_TOO_LONG":            true,
	"SESSION_PASSWORD_NEEDED":           true,
	"INPUT_FETCH_FAIL":                  true,
	"CONNECTION_SYSTEM_EMPTY":           true,
	"FILE_WRITE_FAILED":                 true,
	"STORAGE_CHOOSE_VOLUME_FAILED":      true,
	"xxx":                               true,
	"AES_DECRYPT_FAILED":                true,
	"Timedout":                          true,
	"SEND_REACTION_RESULT1_INVALID":     true,
	"TEMPNAM_FAILED":                    true,
	"MSG_WAIT_TIMEOUT":                  true,
	"MEMBER_CHAT_ADD_FAILED":            true,
	"CHAT_FROM_CALL_CHANGED":            true,
	"MTPROTO_CLUSTER_INVALID":           true,
	"CONNECTION_DEVICE_MODEL_EMPTY":     true,
	"AUTH_KEY_PERM_EMPTY":               true,
	"UNKNOWN_METHOD":                    true,
	"ENCRYPTION_OCCUPY_FAILED":          true,
	"ENCRYPTION_OCCUPY_ADMIN_FAILED":    true,
	"CHAT_OCCUPY_USERNAME_FAILED":       true,
	"REG_ID_GENERATE_FAILED":            true,
	"CONNECTION_LANG_PACK_INVALID":      true,
	"MSGID_DECREASE_RETRY":              true,
	"API_CALL_ERROR":                    true,
	"STORAGE_CHECK_FAILED":              true,
	"INPUT_LAYER_INVALID":               true,
	"NEED_MEMBER_INVALID":               true,
	"NEED_CHAT_INVALID":                 true,
	"HISTORY_GET_FAILED":                true,
	"CHP_CALL_FAIL":                     true,
	"IMAGE_ENGINE_DOWN":                 true,
	"MSG_RANGE_UNSYNC":                  true,
	"NEED_OWNER_EMPTY":                  true,
	"PTS_CHANGE_EMPTY":                  true,
	"CONNECTION_SYSTEM_LANG_CODE_EMPTY": true,
	"WORKER_BUSY_TOO_LONG_RETRY":        true,
	"WP_ID_GENERATE_FAILED":             true,
	"ARR_CAS_FAILED":                    true,
	"CHANNEL_ADD_INVALID":               true,
	"CHANNEL_ADMINS_INVALID":            true,
	"CHAT_OCCUPY_LOC_FAILED":            true,
	"GROUPED_ID_OCCUPY_FAILED":          true,
	"GROUPED_ID_OCCUPY_FAULED":          true,
	"LOG_WRAP_FAIL":                     true,
	"MEMBER_FETCH_FAILED":               true,
	"MEMBER_OCCUPY_PRIMARY_LOC_FAILED":  true,
	"MEMBER_NO_LOCATION":                true,
	"MEMBER_OCCUPY_USERNAME_FAILED":     true,
	"MT_SEND_QUEUE_TOO_LONG":            true,
	"POSTPONED_TIMEOUT":                 true,
	"RPC_CONNECT_FAILED":                true,
	"SHORTNAME_OCCUPY_FAILED":           true,
	"STORE_INVALID_OBJECT_TYPE":         true,
	"STORE_INVALID_SCALAR_TYPE":         true,
	"TMSG_ADD_FAILED":                   true,
	"UNKNOWN_ERROR":                     true,
	"UPLOAD_NO_VOLUME":                  true,
	"USER_NOT_AVAILABLE":                true,
	"VOLUME_LOC_NOT_FOUND":              true,
	"FILE_WRITE_EMPTY":                  true,
	"Internal_Server_Error":             true,
	"INVITE_HASH_UNSYNC":                true,
	"CHANNEL_ID_GENERATE_FAILED":        true,
	"Invalid msgs_state_req query":      true,
}

var (
	filePartMissing = regexp.MustCompile(`FILE_PART_\d*_MISSING`)
	// PCRE's $ (no D modifier) also matches before a final newline.
	methodName = regexp.MustCompile(`^[a-zA-Z0-9._]+\n?\z`)
	// PHP 8's is_numeric: whitespace around, a sign, a decimal or an exponent.
	numeric = regexp.MustCompile(`^[ \t\n\r\v\f]*[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?[ \t\n\r\v\f]*\z`)
)

func isNumeric(s string) bool { return numeric.MatchString(s) }

// phpInt is PHP's (int) of a numeric string.
func phpInt(s string) int64 {
	s = strings.Trim(s, " \t\n\r\v\f")
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	f, _ := strconv.ParseFloat(s, 64)
	switch {
	case f != f:
		return 0
	case f >= 9.223372036854775807e18:
		return 1<<63 - 1
	case f <= -9.223372036854775808e18:
		return -1 << 63
	}
	return int64(f)
}

// mysqlInt is the integer MySQL stores for a numeric string in an INT column: decimals are
// rounded half away from zero ("4.7" is 5), where PHP's (int) truncates. The PHP version
// passed the reported code to MySQL as it came.
func mysqlInt(s string) int64 {
	t := strings.Trim(s, " \t\n\r\v\f")
	if n, err := strconv.ParseInt(t, 10, 64); err == nil {
		return n
	}
	f, _ := strconv.ParseFloat(t, 64)
	f = math.Round(f)
	switch {
	case f != f:
		return 0
	case f >= 9.223372036854775807e18:
		return 1<<63 - 1
	case f <= -9.223372036854775808e18:
		return -1 << 63
	}
	return int64(f)
}

// isBad is MadelineProto's RPCErrorException::isBad: an error that isn't a method's own
// (server failures, flood waits, migrations, garbage). Keep in sync with it.
func isBad(err string, code int64, method string) bool {
	_ = code
	return badErrors[err] ||
		strings.Contains(err, "Received bad_msg_notification") ||
		strings.Contains(err, "FLOOD_WAIT_") ||
		strings.Contains(err, "_MIGRATE_") ||
		strings.Contains(err, "INPUT_METHOD_INVALID") ||
		strings.Contains(err, "INPUT_CONSTRUCTOR_INVALID") ||
		strings.Contains(err, "INPUT_FETCH_ERROR_") ||
		strings.Contains(err, "https://telegram.org/dl") ||
		strings.HasPrefix(err, "RECAPTCHA_CHECK_") ||
		strings.HasPrefix(err, "No workers running") ||
		strings.HasPrefix(err, "FLOOD_TEST_PHONE_WAIT_") ||
		strings.HasPrefix(err, "All workers are busy. Active_queries ") ||
		filePartMissing.MatchString(err) ||
		!methodName.MatchString(method) ||
		(err == "Timeout" && !slices.Contains([]string{"messages.getbotcallbackanswer", "messages.getinlinebotresults"}, strings.ToLower(method))) ||
		(err == "BOT_MISSING" && slices.Contains([]string{"stickers.changeStickerPosition", "stickers.createStickerSet", "messages.uploadMedia"}, method)) ||
		isNumeric(method)
}
