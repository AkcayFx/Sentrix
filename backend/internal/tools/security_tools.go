package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/yourorg/sentrix/internal/provider"
	"github.com/yourorg/sentrix/internal/sandbox"
)

const (
	defaultSecurityTimeout = 5 * time.Minute
	maxSecurityTimeout     = 30 * time.Minute
)

// sqlmapDefaultTimeout is the fallback timeout for sqlmap invocations.
// Overridden by SQLMAP_DEFAULT_TIMEOUT_SECONDS env var at init time.
var sqlmapDefaultTimeout = defaultSecurityTimeout

func init() {
	if v := os.Getenv("SQLMAP_DEFAULT_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			sqlmapDefaultTimeout = time.Duration(n) * time.Second
			if sqlmapDefaultTimeout > maxSecurityTimeout {
				sqlmapDefaultTimeout = maxSecurityTimeout
			}
		}
	}
}

type commandSpec struct {
	binary  string
	args    []string
	timeout time.Duration
}

type commandResult struct {
	stdout   string
	stderr   string
	combined string
	duration time.Duration
	exitCode int
	timedOut bool
}

type commandBuilder func(rawArgs json.RawMessage) (commandSpec, error)
type highlightExtractor func(output string) []string

// sandboxProvider gives commandTool access to the sandbox if available.
type sandboxProvider interface {
	SandboxClient() sandbox.Client
	ContainerID() string
}

type commandTool struct {
	name      string
	binary    string
	def       provider.ToolDef
	build     commandBuilder
	extractor highlightExtractor
	sbp       sandboxProvider
}

func (t *commandTool) Name() string { return t.name }

func (t *commandTool) Definition() provider.ToolDef { return t.def }

func (t *commandTool) Binary() string { return t.binary }

func (t *commandTool) IsAvailable() bool {
	// If sandbox is active, the binary is inside the container image.
	if t.sbp != nil && t.sbp.SandboxClient() != nil && t.sbp.ContainerID() != "" {
		return true
	}
	return isBinaryAvailable(t.binary)
}

func (t *commandTool) Handle(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	spec, err := t.build(rawArgs)
	if err != nil {
		return "", err
	}

	if spec.binary == "" {
		spec.binary = t.binary
	}
	if spec.timeout <= 0 {
		spec.timeout = defaultSecurityTimeout
	}

	var result commandResult

	// Route through sandbox if available.
	if t.sbp != nil && t.sbp.SandboxClient() != nil && t.sbp.ContainerID() != "" {
		result, err = runCommandSandbox(ctx, t.sbp.SandboxClient(), t.sbp.ContainerID(), spec)
	} else {
		result, err = runCommand(ctx, spec)
	}
	if err != nil {
		return "", err
	}

	var highlights []string
	if t.extractor != nil {
		highlights = t.extractor(result.combined)
	}

	return summarizeCommandExecution(t.name, spec, result, highlights), nil
}

// runCommandSandbox executes a command inside a Docker sandbox container.
func runCommandSandbox(ctx context.Context, sb sandbox.Client, containerID string, spec commandSpec) (commandResult, error) {
	cmd := append([]string{spec.binary}, spec.args...)
	execResult, err := sb.Exec(ctx, containerID, cmd, spec.timeout)
	if err != nil {
		return commandResult{}, fmt.Errorf("sandbox exec %s: %w", spec.binary, err)
	}

	result := commandResult{
		stdout:   execResult.Stdout,
		stderr:   execResult.Stderr,
		duration: execResult.Duration,
		exitCode: execResult.ExitCode,
		timedOut: execResult.TimedOut,
	}
	if result.stdout != "" && result.stderr != "" {
		result.combined = result.stdout + "\n\n--- STDERR ---\n" + result.stderr
	} else if result.stdout != "" {
		result.combined = result.stdout
	} else if result.stderr != "" {
		result.combined = result.stderr
	}

	return result, nil
}

func runCommand(ctx context.Context, spec commandSpec) (commandResult, error) {
	runCtx, cancel := context.WithTimeout(ctx, spec.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.binary, spec.args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := commandResult{
		stdout:   strings.TrimSpace(stdout.String()),
		stderr:   strings.TrimSpace(stderr.String()),
		duration: duration,
		exitCode: 0,
	}
	if result.stdout != "" && result.stderr != "" {
		result.combined = result.stdout + "\n\n--- STDERR ---\n" + result.stderr
	} else if result.stdout != "" {
		result.combined = result.stdout
	} else if result.stderr != "" {
		result.combined = result.stderr
	}

	if err == nil {
		return result, nil
	}

	if runCtx.Err() == context.DeadlineExceeded {
		result.timedOut = true
		result.exitCode = -1
		return result, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.exitCode = exitErr.ExitCode()
		return result, nil
	}

	return result, fmt.Errorf("run %s: %w", spec.binary, err)
}

func summarizeCommandExecution(name string, spec commandSpec, result commandResult, highlights []string) string {
	status := "success"
	switch {
	case result.timedOut:
		status = fmt.Sprintf("timed out after %s", spec.timeout)
	case result.exitCode != 0:
		status = fmt.Sprintf("exit code %d", result.exitCode)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", strings.ToUpper(name))
	fmt.Fprintf(&b, "Status: %s\n", status)
	fmt.Fprintf(&b, "Command: `%s`\n", renderCommand(spec.binary, spec.args))
	fmt.Fprintf(&b, "Duration: %s\n", result.duration.Round(time.Millisecond))

	if len(highlights) > 0 {
		b.WriteString("\nHighlights:\n")
		for _, highlight := range highlights {
			fmt.Fprintf(&b, "- %s\n", highlight)
		}
	}

	excerpt := strings.TrimSpace(result.combined)
	if excerpt == "" {
		excerpt = "(no output)"
	}

	b.WriteString("\nOutput excerpt:\n```text\n")
	b.WriteString(truncateRunes(excerpt, 6000))
	if !strings.HasSuffix(excerpt, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n")

	return b.String()
}

func renderCommand(binary string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteCommandPart(binary))
	for _, arg := range args {
		parts = append(parts, quoteCommandPart(arg))
	}
	return strings.Join(parts, " ")
}

func quoteCommandPart(part string) string {
	if part == "" {
		return `""`
	}
	if strings.IndexFunc(part, unicode.IsSpace) == -1 && !strings.ContainsAny(part, `"'`) {
		return part
	}
	return strconv.Quote(part)
}

func timeoutSeconds(raw Int64, fallback time.Duration) time.Duration {
	if raw.Int() <= 0 {
		return fallback
	}
	timeout := time.Duration(raw.Int()) * time.Second
	if timeout > maxSecurityTimeout {
		return maxSecurityTimeout
	}
	return timeout
}

func appendExtraArgs(args []string, flags string) ([]string, error) {
	extra, err := splitCommandLine(flags)
	if err != nil {
		return nil, err
	}
	return append(args, extra...), nil
}

func splitCommandLine(input string) ([]string, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}

	var args []string
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for _, r := range input {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in flags: %s", input)
	}
	flush()
	return args, nil
}

// shelljoin quotes each argument for safe embedding in a shell command string.
func shelljoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n... [truncated]"
}

func uniqueStrings(values []string, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, min(limit, len(values)))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) == limit {
			break
		}
	}
	return out
}

func extractLines(output string, re *regexp.Regexp, format func([]string) string, limit int) []string {
	lines := strings.Split(output, "\n")
	var matches []string
	for _, line := range lines {
		match := re.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) == 0 {
			continue
		}
		matches = append(matches, format(match))
	}
	return uniqueStrings(matches, limit)
}

func extractInterestingLines(output string, keywords []string, limit int) []string {
	lowerKeywords := make([]string, len(keywords))
	for i, keyword := range keywords {
		lowerKeywords[i] = strings.ToLower(keyword)
	}

	lines := strings.Split(output, "\n")
	var highlights []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, keyword := range lowerKeywords {
			if strings.Contains(lower, keyword) {
				highlights = append(highlights, trimmed)
				break
			}
		}
	}
	return uniqueStrings(highlights, limit)
}

func extractSimpleCount(prefix string, output string, limit int) []string {
	lines := strings.Split(output, "\n")
	var values []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		values = append(values, trimmed)
	}
	values = uniqueStrings(values, limit)
	if len(values) == 0 {
		return nil
	}
	summary := fmt.Sprintf("%s: %d item(s)", prefix, len(values))
	return append([]string{summary}, values...)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// resolveToIP returns the input as-is if it's already an IP or CIDR.
// Otherwise it resolves the hostname to its first IPv4 address.
func resolveToIP(target string) (string, error) {
	target = strings.TrimSpace(target)

	if net.ParseIP(target) != nil {
		return target, nil
	}
	if _, _, err := net.ParseCIDR(target); err == nil {
		return target, nil
	}

	addrs, err := net.LookupHost(target)
	if err != nil {
		return "", fmt.Errorf("DNS lookup failed for %q: %w", target, err)
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil && ip.To4() != nil {
			return addr, nil
		}
	}
	if len(addrs) > 0 {
		return addrs[0], nil
	}
	return "", fmt.Errorf("no addresses found for %q", target)
}

func newNmapTool() Tool {
	return &commandTool{
		name:      "nmap",
		binary:    "nmap",
		def:       nmapDef(),
		build:     buildNmapCommand,
		extractor: extractNmapHighlights,
	}
}

func newMasscanTool() Tool {
	return &commandTool{
		name:      "masscan",
		binary:    "masscan",
		def:       masscanDef(),
		build:     buildMasscanCommand,
		extractor: extractMasscanHighlights,
	}
}

func newSubfinderTool() Tool {
	return &commandTool{
		name:      "subfinder",
		binary:    "subfinder",
		def:       subfinderDef(),
		build:     buildSubfinderCommand,
		extractor: func(output string) []string { return extractSimpleCount("Subdomains", output, 6) },
	}
}

func newAmassTool() Tool {
	return &commandTool{
		name:   "amass",
		binary: "amass",
		def:    amassDef(),
		build:  buildAmassCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"name", "asn", "subdomain", "discovered"}, 8)
		},
	}
}

func newNiktoTool() Tool {
	return &commandTool{
		name:   "nikto",
		binary: "nikto",
		def:    niktoDef(),
		build:  buildNiktoCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"+ ", "osvdb", "server", "cookie", "vulnerable"}, 8)
		},
	}
}

func newWapitiTool() Tool {
	return &commandTool{
		name:   "wapiti",
		binary: "wapiti",
		def:    wapitiDef(),
		build:  buildWapitiCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"vulnerability", "anomaly", "warning", "found"}, 8)
		},
	}
}

func newSQLMapTool() Tool {
	return &commandTool{
		name:   "sqlmap",
		binary: "sqlmap",
		def:    sqlmapDef(),
		build:  buildSQLMapCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"injectable", "available databases", "current user", "back-end dbms", "waf"}, 8)
		},
	}
}

func newXSStrikeTool() Tool {
	return &commandTool{
		name:   "xsstrike",
		binary: "xsstrike",
		def:    xsstrikeDef(),
		build:  buildXSStrikeCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"payload", "efficiency", "reflection", "vulnerable", "param"}, 8)
		},
	}
}

func newMetasploitTool() Tool {
	return &commandTool{
		name:   "metasploit",
		binary: "msfconsole",
		def:    metasploitDef(),
		build:  buildMetasploitCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"session", "exploit completed", "meterpreter", "target appears"}, 8)
		},
	}
}

func newSearchsploitTool() Tool {
	return &commandTool{
		name:   "searchsploit",
		binary: "searchsploit",
		def:    searchsploitDef(),
		build:  buildSearchsploitCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"edb-id", "cve", "remote", "dos", "exploit"}, 8)
		},
	}
}

func newHydraTool() Tool {
	return &commandTool{
		name:   "hydra",
		binary: "hydra",
		def:    hydraDef(),
		build:  buildHydraCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"login:", "password:", "valid", "success"}, 8)
		},
	}
}

func newJohnTool() Tool {
	return &commandTool{
		name:   "john",
		binary: "john",
		def:    johnDef(),
		build:  buildJohnCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"loaded", "password hash", "guesses", "cracked"}, 8)
		},
	}
}

func newHashcatTool() Tool {
	return &commandTool{
		name:   "hashcat",
		binary: "hashcat",
		def:    hashcatDef(),
		build:  buildHashcatCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"recovered", "speed.#", "status", "guess"}, 8)
		},
	}
}

func newTSharkTool() Tool {
	return &commandTool{
		name:   "tshark",
		binary: "tshark",
		def:    tsharkDef(),
		build:  buildTSharkCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"frame", "http", "dns", "tls", "warning"}, 8)
		},
	}
}

func newTCPDumpTool() Tool {
	return &commandTool{
		name:   "tcpdump",
		binary: "tcpdump",
		def:    tcpdumpDef(),
		build:  buildTCPDumpCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"listening", "packet", "tcp", "udp", "icmp"}, 8)
		},
	}
}

func newTheHarvesterTool() Tool {
	return &commandTool{
		name:   "theharvester",
		binary: "theHarvester",
		def:    theharvesterDef(),
		build:  buildTheHarvesterCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"hosts found", "emails found", "ip", "host"}, 8)
		},
	}
}

func newReconNGTool() Tool {
	return &commandTool{
		name:   "recon_ng",
		binary: "recon-ng",
		def:    reconNgDef(),
		build:  buildReconNGCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"loaded", "source", "hosts", "contacts", "domains"}, 8)
		},
	}
}

func newFFUFTool() Tool {
	return &commandTool{
		name:   "ffuf",
		binary: "ffuf",
		def:    ffufDef(),
		build:  buildFFUFCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"status:", "size:", "words:", "lines:", "redirect"}, 8)
		},
	}
}

func newGobusterTool() Tool {
	return &commandTool{
		name:   "gobuster",
		binary: "gobuster",
		def:    gobusterDef(),
		build:  buildGobusterCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"found:", "status:", "progress", "subdomain"}, 8)
		},
	}
}

func newWFuzzTool() Tool {
	return &commandTool{
		name:   "wfuzz",
		binary: "wfuzz",
		def:    wfuzzDef(),
		build:  buildWFuzzCommand,
		extractor: func(output string) []string {
			return extractInterestingLines(output, []string{"code", "lines", "words", "payload", "seed"}, 8)
		},
	}
}

func nmapDef() provider.ToolDef {
	return toolSchema("nmap",
		"Run nmap for host discovery, port scanning, and service fingerprinting.",
		map[string]interface{}{
			"target":    prop("string", "IP, hostname, or CIDR target to scan"),
			"ports":     prop("string", "Optional port list or range such as 80,443 or 1-1024"),
			"scan_type": propEnum("Scan profile", []string{"syn", "connect", "version", "aggressive", "udp"}),
			"flags":     prop("string", "Optional additional raw nmap flags"),
			"timeout":   prop("integer", "Timeout in seconds"),
			"message":   prop("string", "Brief purpose of the scan"),
		},
		[]string{"target"},
	)
}

func masscanDef() provider.ToolDef {
	return toolSchema("masscan",
		"Run masscan for high-speed port discovery against a host or network range. Hostnames are automatically resolved to IP addresses.",
		map[string]interface{}{
			"target":  prop("string", "IP, hostname, or CIDR target to scan (hostnames are auto-resolved)"),
			"ports":   prop("string", "Optional port list or range"),
			"rate":    prop("integer", "Packets per second rate limit"),
			"flags":   prop("string", "Optional additional raw masscan flags"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"target"},
	)
}

func subfinderDef() provider.ToolDef {
	return toolSchema("subfinder",
		"Enumerate subdomains for a given domain with passive sources.",
		map[string]interface{}{
			"domain":  prop("string", "Root domain to enumerate"),
			"flags":   prop("string", "Optional additional raw subfinder flags"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"domain"},
	)
}

func amassDef() provider.ToolDef {
	return toolSchema("amass",
		"Run amass for attack surface mapping and subdomain enumeration.",
		map[string]interface{}{
			"domain":  prop("string", "Root domain to enumerate"),
			"mode":    propEnum("Amass mode", []string{"enum", "intel", "track"}),
			"flags":   prop("string", "Optional additional raw amass flags"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"domain"},
	)
}

func niktoDef() provider.ToolDef {
	return toolSchema("nikto",
		"Run Nikto against a web server to identify misconfigurations and common issues.",
		map[string]interface{}{
			"target":  prop("string", "Target hostname, IP, or URL"),
			"port":    prop("integer", "Optional port override"),
			"flags":   prop("string", "Optional additional raw Nikto flags"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"target"},
	)
}

func wapitiDef() provider.ToolDef {
	return toolSchema("wapiti",
		"Run Wapiti against a web application URL for lightweight vulnerability discovery.",
		map[string]interface{}{
			"url":     prop("string", "Target URL"),
			"scope":   propEnum("Scan scope", []string{"folder", "domain", "url", "page", "punk"}),
			"flags":   prop("string", "Optional additional raw Wapiti flags"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"url"},
	)
}

func sqlmapDef() provider.ToolDef {
	return toolSchema("sqlmap",
		"Run sqlmap for SQL injection detection and database fingerprinting.",
		map[string]interface{}{
			"url":     prop("string", "Target URL"),
			"data":    prop("string", "Optional POST body"),
			"param":   prop("string", "Optional parameter name to focus on"),
			"level":   prop("integer", "Optional sqlmap level"),
			"risk":    prop("integer", "Optional sqlmap risk"),
			"timeout": prop("integer", "Optional timeout in seconds (default: 300). Use a shorter value (e.g. 60-120) for fast probes."),
			"flags":   prop("string", "Optional additional raw sqlmap flags"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"url"},
	)
}

func xsstrikeDef() provider.ToolDef {
	return toolSchema("xsstrike",
		"Run XSStrike for focused reflected and DOM XSS testing.",
		map[string]interface{}{
			"url":     prop("string", "Target URL"),
			"param":   prop("string", "Optional parameter name to focus on"),
			"flags":   prop("string", "Optional additional raw XSStrike flags"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"url"},
	)
}

func metasploitDef() provider.ToolDef {
	return toolSchema("metasploit",
		"Run a scripted Metasploit console command sequence.",
		map[string]interface{}{
			"command": prop("string", "Metasploit console commands to execute"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"command"},
	)
}

func searchsploitDef() provider.ToolDef {
	return toolSchema("searchsploit",
		"Search Exploit-DB entries with searchsploit.",
		map[string]interface{}{
			"query":   prop("string", "Search query"),
			"flags":   prop("string", "Optional additional raw searchsploit flags"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"query"},
	)
}

func hydraDef() provider.ToolDef {
	return toolSchema("hydra",
		"Run Hydra for network login testing against a scoped service.",
		map[string]interface{}{
			"target":    prop("string", "Target hostname or IP"),
			"service":   prop("string", "Service name such as ssh, ftp, http-get, rdp"),
			"username":  prop("string", "Single username"),
			"user_list": prop("string", "Path to username list"),
			"pass_list": prop("string", "Path to password list"),
			"flags":     prop("string", "Optional additional raw Hydra flags"),
			"message":   prop("string", "Brief purpose of the scan"),
		},
		[]string{"target", "service", "pass_list"},
	)
}

func johnDef() provider.ToolDef {
	return toolSchema("john",
		"Run John the Ripper against a hash file.",
		map[string]interface{}{
			"hash_file": prop("string", "Path to the hash file"),
			"format":    prop("string", "Optional hash format"),
			"wordlist":  prop("string", "Optional wordlist path"),
			"flags":     prop("string", "Optional additional raw John flags"),
			"message":   prop("string", "Brief purpose of the scan"),
		},
		[]string{"hash_file"},
	)
}

func hashcatDef() provider.ToolDef {
	return toolSchema("hashcat",
		"Run Hashcat against a hash file for password recovery.",
		map[string]interface{}{
			"hash_file":   prop("string", "Path to the hash file"),
			"attack_mode": propEnum("Hashcat attack mode", []string{"0", "1", "3", "6", "7"}),
			"hash_type":   prop("string", "Hashcat mode identifier, for example 0 or 1800"),
			"wordlist":    prop("string", "Optional wordlist or mask argument"),
			"flags":       prop("string", "Optional additional raw Hashcat flags"),
			"message":     prop("string", "Brief purpose of the scan"),
		},
		[]string{"hash_file", "hash_type"},
	)
}

func tsharkDef() provider.ToolDef {
	return toolSchema("tshark",
		"Run tshark against a live interface or capture file.",
		map[string]interface{}{
			"interface":   prop("string", "Interface name for live capture"),
			"read_file":   prop("string", "Optional pcap or pcapng file to read"),
			"filter":      prop("string", "Optional capture or display filter"),
			"max_packets": prop("integer", "Optional packet limit"),
			"flags":       prop("string", "Optional additional raw tshark flags"),
			"message":     prop("string", "Brief purpose of the scan"),
		},
		nil,
	)
}

func tcpdumpDef() provider.ToolDef {
	return toolSchema("tcpdump",
		"Run tcpdump against a live interface or save traffic to a file.",
		map[string]interface{}{
			"interface":   prop("string", "Interface name for live capture"),
			"filter":      prop("string", "Optional BPF filter"),
			"max_packets": prop("integer", "Optional packet limit"),
			"write_file":  prop("string", "Optional pcap output file"),
			"flags":       prop("string", "Optional additional raw tcpdump flags"),
			"message":     prop("string", "Brief purpose of the scan"),
		},
		nil,
	)
}

func theharvesterDef() provider.ToolDef {
	return toolSchema("theharvester",
		"Run theHarvester for public email, host, and domain OSINT.",
		map[string]interface{}{
			"domain":  prop("string", "Domain to investigate"),
			"source":  prop("string", "Source backend such as all, bing, duckduckgo, crtsh"),
			"limit":   prop("integer", "Result limit"),
			"message": prop("string", "Brief purpose of the scan"),
		},
		[]string{"domain"},
	)
}

func reconNgDef() provider.ToolDef {
	return toolSchema("recon_ng",
		"Run a scripted recon-ng module inside a named workspace.",
		map[string]interface{}{
			"module":    prop("string", "Module name such as recon/domains-hosts/bing_domain_web"),
			"workspace": prop("string", "Workspace name"),
			"options":   prop("string", "Optional raw module options string"),
			"message":   prop("string", "Brief purpose of the scan"),
		},
		[]string{"module"},
	)
}

func ffufDef() provider.ToolDef {
	return toolSchema("ffuf",
		"Run ffuf for web content discovery or parameter fuzzing.",
		map[string]interface{}{
			"url":      prop("string", "Target URL containing FUZZ where applicable"),
			"wordlist": prop("string", "Wordlist path"),
			"method":   propEnum("HTTP method", []string{"GET", "POST", "PUT", "DELETE", "HEAD"}),
			"filters":  prop("string", "Optional raw filter arguments such as -fc 404"),
			"flags":    prop("string", "Optional additional raw ffuf flags"),
			"message":  prop("string", "Brief purpose of the scan"),
		},
		[]string{"url", "wordlist"},
	)
}

func gobusterDef() provider.ToolDef {
	return toolSchema("gobuster",
		"Run gobuster for directory, DNS, or virtual host enumeration.",
		map[string]interface{}{
			"url":      prop("string", "Target URL or domain depending on mode"),
			"wordlist": prop("string", "Wordlist path"),
			"mode":     propEnum("Gobuster mode", []string{"dir", "dns", "vhost"}),
			"flags":    prop("string", "Optional additional raw gobuster flags"),
			"message":  prop("string", "Brief purpose of the scan"),
		},
		[]string{"url", "wordlist"},
	)
}

func wfuzzDef() provider.ToolDef {
	return toolSchema("wfuzz",
		"Run wfuzz for targeted web fuzzing and parameter discovery.",
		map[string]interface{}{
			"url":      prop("string", "Target URL containing FUZZ where applicable"),
			"wordlist": prop("string", "Wordlist path"),
			"param":    prop("string", "Optional parameter payload string"),
			"filters":  prop("string", "Optional raw filter arguments"),
			"message":  prop("string", "Brief purpose of the scan"),
		},
		[]string{"url", "wordlist"},
	)
}

func buildNmapCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args NmapArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse nmap args: %w", err)
	}
	if strings.TrimSpace(args.Target) == "" {
		return commandSpec{}, fmt.Errorf("nmap target is required")
	}

	cmdArgs := []string{"-Pn"}
	switch args.ScanType {
	case "syn":
		cmdArgs = append(cmdArgs, "-sS")
	case "connect":
		cmdArgs = append(cmdArgs, "-sT")
	case "version":
		cmdArgs = append(cmdArgs, "-sV")
	case "aggressive":
		cmdArgs = append(cmdArgs, "-A")
	case "udp":
		cmdArgs = append(cmdArgs, "-sU")
	default:
		cmdArgs = append(cmdArgs, "-sV")
	}
	if args.Ports != "" {
		cmdArgs = append(cmdArgs, "-p", args.Ports)
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	cmdArgs = append(cmdArgs, args.Target)

	return commandSpec{
		binary:  "nmap",
		args:    cmdArgs,
		timeout: timeoutSeconds(args.Timeout, defaultSecurityTimeout),
	}, nil
}

func buildMasscanCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args MasscanArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse masscan args: %w", err)
	}
	if strings.TrimSpace(args.Target) == "" {
		return commandSpec{}, fmt.Errorf("masscan target is required")
	}

	target, err := resolveToIP(args.Target)
	if err != nil {
		return commandSpec{}, fmt.Errorf("masscan: resolve target %q: %w", args.Target, err)
	}

	cmdArgs := []string{target}
	if args.Ports != "" {
		cmdArgs = append(cmdArgs, "-p", args.Ports)
	}
	if args.Rate.Int() > 0 {
		cmdArgs = append(cmdArgs, "--rate", strconv.Itoa(args.Rate.Int()))
	} else {
		cmdArgs = append(cmdArgs, "--rate", "1000")
	}
	// Inside Docker containers masscan often hangs trying to auto-detect
	// the source IP via ARP. Wrap the call so we resolve the adapter IP
	// from the default route and pass --adapter-ip explicitly.
	cmdArgs = append(cmdArgs, "--wait", "3")
	cmdArgs, extraErr := appendExtraArgs(cmdArgs, args.Flags)
	if extraErr != nil {
		return commandSpec{}, extraErr
	}
	// Use a shell wrapper that injects --adapter-ip from the container's
	// default interface, preventing masscan from hanging on ARP resolution.
	shellCmd := fmt.Sprintf(
		`ADAPTER_IP=$(ip -4 addr show dev eth0 2>/dev/null | grep -oP 'inet \K[0-9.]+' | head -1); `+
			`exec masscan %s ${ADAPTER_IP:+--adapter-ip "$ADAPTER_IP"}`,
		shelljoin(cmdArgs),
	)
	return commandSpec{binary: "sh", args: []string{"-c", shellCmd}, timeout: 3 * time.Minute}, nil
}

func buildSubfinderCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args SubfinderArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse subfinder args: %w", err)
	}
	if strings.TrimSpace(args.Domain) == "" {
		return commandSpec{}, fmt.Errorf("subfinder domain is required")
	}

	cmdArgs := []string{"-d", args.Domain, "-silent"}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "subfinder", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildAmassCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args AmassArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse amass args: %w", err)
	}
	if strings.TrimSpace(args.Domain) == "" {
		return commandSpec{}, fmt.Errorf("amass domain is required")
	}

	mode := args.Mode
	if mode == "" {
		mode = "enum"
	}
	cmdArgs := []string{mode, "-d", args.Domain}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "amass", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildNiktoCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args NiktoArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse nikto args: %w", err)
	}
	if strings.TrimSpace(args.Target) == "" {
		return commandSpec{}, fmt.Errorf("nikto target is required")
	}

	cmdArgs := []string{"-h", args.Target}
	if args.Port.Int() > 0 {
		cmdArgs = append(cmdArgs, "-p", strconv.Itoa(args.Port.Int()))
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "nikto", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildWapitiCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args WapitiArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse wapiti args: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" {
		return commandSpec{}, fmt.Errorf("wapiti url is required")
	}

	cmdArgs := []string{"-u", args.URL}
	if args.Scope != "" {
		cmdArgs = append(cmdArgs, "--scope", args.Scope)
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "wapiti", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildSQLMapCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args SqlmapArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse sqlmap args: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" {
		return commandSpec{}, fmt.Errorf("sqlmap url is required")
	}

	cmdArgs := []string{"-u", args.URL, "--batch"}
	if args.Data != "" {
		cmdArgs = append(cmdArgs, "--data", args.Data)
	}
	if args.Param != "" {
		cmdArgs = append(cmdArgs, "-p", args.Param)
	}
	if args.Level.Int() > 0 {
		cmdArgs = append(cmdArgs, "--level", strconv.Itoa(args.Level.Int()))
	}
	if args.Risk.Int() > 0 {
		cmdArgs = append(cmdArgs, "--risk", strconv.Itoa(args.Risk.Int()))
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "sqlmap", args: cmdArgs, timeout: timeoutSeconds(args.Timeout, sqlmapDefaultTimeout)}, nil
}

func buildXSStrikeCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args XSStrikeArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse xsstrike args: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" {
		return commandSpec{}, fmt.Errorf("xsstrike url is required")
	}

	cmdArgs := []string{"-u", args.URL}
	if args.Param != "" {
		cmdArgs = append(cmdArgs, "--params", args.Param)
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "xsstrike", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildMetasploitCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args MetasploitArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse metasploit args: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return commandSpec{}, fmt.Errorf("metasploit command is required")
	}
	return commandSpec{
		binary:  "msfconsole",
		args:    []string{"-q", "-x", args.Command + "; exit -y"},
		timeout: 5 * time.Minute,
	}, nil
}

func buildSearchsploitCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args SearchsploitArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse searchsploit args: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return commandSpec{}, fmt.Errorf("searchsploit query is required")
	}

	cmdArgs := []string{args.Query}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "searchsploit", args: cmdArgs, timeout: 2 * time.Minute}, nil
}

func buildHydraCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args HydraArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse hydra args: %w", err)
	}
	if strings.TrimSpace(args.Target) == "" || strings.TrimSpace(args.Service) == "" {
		return commandSpec{}, fmt.Errorf("hydra target and service are required")
	}
	if strings.TrimSpace(args.Username) == "" && strings.TrimSpace(args.UserList) == "" {
		return commandSpec{}, fmt.Errorf("hydra requires username or user_list")
	}
	if strings.TrimSpace(args.PassList) == "" {
		return commandSpec{}, fmt.Errorf("hydra requires pass_list")
	}

	cmdArgs := []string{}
	if args.Username != "" {
		cmdArgs = append(cmdArgs, "-l", args.Username)
	}
	if args.UserList != "" {
		cmdArgs = append(cmdArgs, "-L", args.UserList)
	}
	cmdArgs = append(cmdArgs, "-P", args.PassList)
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	cmdArgs = append(cmdArgs, args.Target, args.Service)
	return commandSpec{binary: "hydra", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildJohnCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args JohnArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse john args: %w", err)
	}
	if strings.TrimSpace(args.HashFile) == "" {
		return commandSpec{}, fmt.Errorf("john hash_file is required")
	}

	cmdArgs := []string{args.HashFile}
	if args.Format != "" {
		cmdArgs = append(cmdArgs, "--format="+args.Format)
	}
	if args.Wordlist != "" {
		cmdArgs = append(cmdArgs, "--wordlist="+args.Wordlist)
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "john", args: cmdArgs, timeout: 5 * time.Minute}, nil
}

func buildHashcatCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args HashcatArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse hashcat args: %w", err)
	}
	if strings.TrimSpace(args.HashFile) == "" || strings.TrimSpace(args.HashType) == "" {
		return commandSpec{}, fmt.Errorf("hashcat hash_file and hash_type are required")
	}

	attackMode := args.AttackMode
	if attackMode == "" {
		attackMode = "0"
	}

	cmdArgs := []string{"-m", args.HashType, "-a", attackMode, args.HashFile}
	if args.Wordlist != "" {
		cmdArgs = append(cmdArgs, args.Wordlist)
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "hashcat", args: cmdArgs, timeout: 5 * time.Minute}, nil
}

func buildTSharkCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args TsharkArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse tshark args: %w", err)
	}
	if args.Interface == "" && args.ReadFile == "" {
		return commandSpec{}, fmt.Errorf("tshark requires interface or read_file")
	}

	cmdArgs := []string{}
	if args.Interface != "" {
		cmdArgs = append(cmdArgs, "-i", args.Interface)
	}
	if args.ReadFile != "" {
		cmdArgs = append(cmdArgs, "-r", args.ReadFile)
	}
	if args.Filter != "" {
		cmdArgs = append(cmdArgs, "-Y", args.Filter)
	}
	if args.MaxPackets.Int() > 0 {
		cmdArgs = append(cmdArgs, "-c", strconv.Itoa(args.MaxPackets.Int()))
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "tshark", args: cmdArgs, timeout: 5 * time.Minute}, nil
}

func buildTCPDumpCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args TcpdumpArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse tcpdump args: %w", err)
	}
	if args.Interface == "" {
		return commandSpec{}, fmt.Errorf("tcpdump requires interface")
	}

	cmdArgs := []string{"-i", args.Interface}
	if args.MaxPackets.Int() > 0 {
		cmdArgs = append(cmdArgs, "-c", strconv.Itoa(args.MaxPackets.Int()))
	}
	if args.WriteFile != "" {
		cmdArgs = append(cmdArgs, "-w", args.WriteFile)
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	if args.Filter != "" {
		filterParts, err := splitCommandLine(args.Filter)
		if err != nil {
			return commandSpec{}, err
		}
		cmdArgs = append(cmdArgs, filterParts...)
	}
	return commandSpec{binary: "tcpdump", args: cmdArgs, timeout: 5 * time.Minute}, nil
}

func buildTheHarvesterCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args TheHarvesterArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse theHarvester args: %w", err)
	}
	if strings.TrimSpace(args.Domain) == "" {
		return commandSpec{}, fmt.Errorf("theHarvester domain is required")
	}

	source := args.Source
	if source == "" {
		source = "all"
	}
	cmdArgs := []string{"-d", args.Domain, "-b", source}
	if args.Limit.Int() > 0 {
		cmdArgs = append(cmdArgs, "-l", strconv.Itoa(args.Limit.Int()))
	}
	return commandSpec{binary: "theHarvester", args: cmdArgs, timeout: 5 * time.Minute}, nil
}

func buildReconNGCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args ReconNgArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse recon-ng args: %w", err)
	}
	if strings.TrimSpace(args.Module) == "" {
		return commandSpec{}, fmt.Errorf("recon-ng module is required")
	}

	workspace := args.Workspace
	if workspace == "" {
		workspace = "default"
	}

	cmdArgs := []string{"-w", workspace, "-m", args.Module}
	if strings.TrimSpace(args.Options) != "" {
		cmdArgs = append(cmdArgs, "-o", args.Options)
	}
	cmdArgs = append(cmdArgs, "-x")
	return commandSpec{binary: "recon-ng", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildFFUFCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args FfufArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse ffuf args: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" || strings.TrimSpace(args.Wordlist) == "" {
		return commandSpec{}, fmt.Errorf("ffuf url and wordlist are required")
	}

	cmdArgs := []string{"-u", args.URL, "-w", args.Wordlist}
	if args.Method != "" {
		cmdArgs = append(cmdArgs, "-X", args.Method)
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Filters)
	if err != nil {
		return commandSpec{}, err
	}
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "ffuf", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildGobusterCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args GobusterArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse gobuster args: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" || strings.TrimSpace(args.Wordlist) == "" {
		return commandSpec{}, fmt.Errorf("gobuster url and wordlist are required")
	}

	mode := args.Mode
	if mode == "" {
		mode = "dir"
	}

	cmdArgs := []string{mode, "-w", args.Wordlist}
	switch mode {
	case "dns":
		cmdArgs = append(cmdArgs, "-d", args.URL)
	case "vhost":
		cmdArgs = append(cmdArgs, "-u", args.URL)
	default:
		cmdArgs = append(cmdArgs, "-u", args.URL)
	}

	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Flags)
	if err != nil {
		return commandSpec{}, err
	}
	return commandSpec{binary: "gobuster", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func buildWFuzzCommand(rawArgs json.RawMessage) (commandSpec, error) {
	var args WfuzzArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return commandSpec{}, fmt.Errorf("parse wfuzz args: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" || strings.TrimSpace(args.Wordlist) == "" {
		return commandSpec{}, fmt.Errorf("wfuzz url and wordlist are required")
	}

	cmdArgs := []string{"-w", args.Wordlist}
	if args.Param != "" {
		cmdArgs = append(cmdArgs, "-d", args.Param)
	}
	var err error
	cmdArgs, err = appendExtraArgs(cmdArgs, args.Filters)
	if err != nil {
		return commandSpec{}, err
	}
	cmdArgs = append(cmdArgs, args.URL)
	return commandSpec{binary: "wfuzz", args: cmdArgs, timeout: 3 * time.Minute}, nil
}

func extractNmapHighlights(output string) []string {
	highlights := extractLines(output, regexp.MustCompile(`^(\d+)/(tcp|udp)\s+open\s+([^\s]+)\s*(.*)$`), func(match []string) string {
		service := match[3]
		if strings.TrimSpace(match[4]) != "" {
			service += " " + strings.TrimSpace(match[4])
		}
		return fmt.Sprintf("Open port %s/%s: %s", match[1], match[2], service)
	}, 8)
	if len(highlights) > 0 {
		return highlights
	}
	return extractInterestingLines(output, []string{"open", "host is up", "service info", "os details"}, 8)
}

func extractMasscanHighlights(output string) []string {
	return extractLines(output, regexp.MustCompile(`^open\s+(tcp|udp)\s+(\d+)\s+(.+)$`), func(match []string) string {
		return fmt.Sprintf("Open port %s/%s on %s", match[2], match[1], match[3])
	}, 8)
}
