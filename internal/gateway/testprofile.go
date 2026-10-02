package gateway

import "strings"

// mergeEnv overlays override "KEY=VALUE" pairs onto base, replacing any key
// base already set and appending anything new — base's own order is
// otherwise kept. Used to give a child process the gateway's own baseline
// (TERM, COLORTERM, WMS_GATEWAY_SESSION, ...) with a second instance's own
// config (WMS_TEST_ENV_FILE) layered on top, for the Live/Test picker in
// telnet.go and web.go.
func mergeEnv(base, overrides []string) []string {
	idx := map[string]int{}
	out := append([]string(nil), base...)
	for i, kv := range out {
		if k, _, ok := strings.Cut(kv, "="); ok {
			idx[k] = i
		}
	}
	for _, kv := range overrides {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if i, exists := idx[k]; exists {
			out[i] = kv
		} else {
			idx[k] = len(out)
			out = append(out, kv)
		}
	}
	return out
}
