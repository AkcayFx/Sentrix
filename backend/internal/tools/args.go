package tools

import (
	"fmt"
	"strconv"
	"strings"
)

// Bool is a lenient boolean type that handles LLM quoting quirks
// like "true", true, "True", etc.
type Bool bool

func (b *Bool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.ToLower(string(data)), "' \"\n\r\t")
	switch s {
	case "true", "1", "yes":
		*b = true
	case "false", "0", "no", "":
		*b = false
	default:
		return fmt.Errorf("invalid bool value: %s", s)
	}
	return nil
}

func (b Bool) Bool() bool { return bool(b) }

// Int64 is a lenient integer type that handles LLM quoting quirks.
type Int64 int64

func (i *Int64) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.ToLower(string(data)), "' \"\n\r\t")
	if s == "" || s == "null" {
		*i = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid int value: %s", s)
	}
	*i = Int64(n)
	return nil
}

func (i Int64) Int() int     { return int(i) }
func (i Int64) Int64() int64 { return int64(i) }

// ---------------------------------------------------------------------------
// Core tool argument structs
// ---------------------------------------------------------------------------

// TerminalArgs are the arguments for the terminal_exec tool.
type TerminalArgs struct {
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir,omitempty"`
	Timeout    Int64  `json:"timeout,omitempty"`
	Detach     Bool   `json:"detach,omitempty"`
	Message    string `json:"message"`
}

// FileReadArgs are the arguments for the file_read tool.
type FileReadArgs struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// FileWriteArgs are the arguments for the file_write tool.
type FileWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Message string `json:"message"`
}

// ReportFindingArgs are the arguments for the report_finding tool.
type ReportFindingArgs struct {
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Evidence    string `json:"evidence,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Message     string `json:"message"`
}

// DoneArgs are the arguments for the done tool.
type DoneArgs struct {
	Success Bool   `json:"success"`
	Result  string `json:"result"`
	Message string `json:"message"`
}

// WebSearchArgs are the arguments for the generic web_search tool.
type WebSearchArgs struct {
	Query          string   `json:"query"`
	Provider       string   `json:"provider,omitempty"`
	MaxResults     Int64    `json:"max_results,omitempty"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
	Recency        string   `json:"recency,omitempty"`
	Message        string   `json:"message"`
}

// ProviderSearchArgs are the arguments for provider-specific search tools.
type ProviderSearchArgs struct {
	Query          string   `json:"query"`
	MaxResults     Int64    `json:"max_results,omitempty"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
	Recency        string   `json:"recency,omitempty"`
	Message        string   `json:"message"`
}

// SploitusSearchArgs are the arguments for Sploitus exploit/tool searches.
type SploitusSearchArgs struct {
	Query      string `json:"query"`
	Category   string `json:"category,omitempty"`
	Sort       string `json:"sort,omitempty"`
	MaxResults Int64  `json:"max_results,omitempty"`
	Message    string `json:"message"`
}

// BrowserArgs are the arguments for the browser tool.
type BrowserArgs struct {
	URL     string `json:"url"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

// DelegationArgs are the arguments for internal specialist delegation tools.
type DelegationArgs struct {
	Objective string `json:"objective"`
	Context   string `json:"context,omitempty"`
	Message   string `json:"message"`
}

// ---------------------------------------------------------------------------
// Security tool argument structs
// ---------------------------------------------------------------------------

// NmapArgs for the nmap port scanner.
type NmapArgs struct {
	Target   string `json:"target"`
	Ports    string `json:"ports,omitempty"`
	ScanType string `json:"scan_type,omitempty"`
	Flags    string `json:"flags,omitempty"`
	Timeout  Int64  `json:"timeout,omitempty"`
	Message  string `json:"message"`
}

// MasscanArgs for the masscan fast port scanner.
type MasscanArgs struct {
	Target  string `json:"target"`
	Ports   string `json:"ports,omitempty"`
	Rate    Int64  `json:"rate,omitempty"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// SubfinderArgs for subdomain enumeration.
type SubfinderArgs struct {
	Domain  string `json:"domain"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// AmassArgs for attack surface mapping.
type AmassArgs struct {
	Domain  string `json:"domain"`
	Mode    string `json:"mode,omitempty"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// NiktoArgs for web server scanner.
type NiktoArgs struct {
	Target  string `json:"target"`
	Port    Int64  `json:"port,omitempty"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// WapitiArgs for web vulnerability scanner.
type WapitiArgs struct {
	URL     string `json:"url"`
	Scope   string `json:"scope,omitempty"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// SqlmapArgs for SQL injection detection.
type SqlmapArgs struct {
	URL     string `json:"url"`
	Data    string `json:"data,omitempty"`
	Param   string `json:"param,omitempty"`
	Level   Int64  `json:"level,omitempty"`
	Risk    Int64  `json:"risk,omitempty"`
	Timeout Int64  `json:"timeout,omitempty"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// XSStrikeArgs for XSS detection.
type XSStrikeArgs struct {
	URL     string `json:"url"`
	Param   string `json:"param,omitempty"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// MetasploitArgs for the Metasploit framework.
type MetasploitArgs struct {
	Command string `json:"command"`
	Message string `json:"message"`
}

// SearchsploitArgs for exploit database search.
type SearchsploitArgs struct {
	Query   string `json:"query"`
	Flags   string `json:"flags,omitempty"`
	Message string `json:"message"`
}

// HydraArgs for network login cracking.
type HydraArgs struct {
	Target   string `json:"target"`
	Service  string `json:"service"`
	Username string `json:"username,omitempty"`
	UserList string `json:"user_list,omitempty"`
	PassList string `json:"pass_list,omitempty"`
	Flags    string `json:"flags,omitempty"`
	Message  string `json:"message"`
}

// JohnArgs for password cracking.
type JohnArgs struct {
	HashFile string `json:"hash_file"`
	Format   string `json:"format,omitempty"`
	Wordlist string `json:"wordlist,omitempty"`
	Flags    string `json:"flags,omitempty"`
	Message  string `json:"message"`
}

// HashcatArgs for advanced password recovery.
type HashcatArgs struct {
	HashFile   string `json:"hash_file"`
	AttackMode string `json:"attack_mode,omitempty"`
	HashType   string `json:"hash_type,omitempty"`
	Wordlist   string `json:"wordlist,omitempty"`
	Flags      string `json:"flags,omitempty"`
	Message    string `json:"message"`
}

// TsharkArgs for packet analysis.
type TsharkArgs struct {
	Interface  string `json:"interface,omitempty"`
	ReadFile   string `json:"read_file,omitempty"`
	Filter     string `json:"filter,omitempty"`
	MaxPackets Int64  `json:"max_packets,omitempty"`
	Flags      string `json:"flags,omitempty"`
	Message    string `json:"message"`
}

// TcpdumpArgs for packet capture.
type TcpdumpArgs struct {
	Interface  string `json:"interface,omitempty"`
	Filter     string `json:"filter,omitempty"`
	MaxPackets Int64  `json:"max_packets,omitempty"`
	WriteFile  string `json:"write_file,omitempty"`
	Flags      string `json:"flags,omitempty"`
	Message    string `json:"message"`
}

// TheHarvesterArgs for email/domain OSINT.
type TheHarvesterArgs struct {
	Domain  string `json:"domain"`
	Source  string `json:"source,omitempty"`
	Limit   Int64  `json:"limit,omitempty"`
	Message string `json:"message"`
}

// ReconNgArgs for the reconnaissance framework.
type ReconNgArgs struct {
	Module    string `json:"module"`
	Workspace string `json:"workspace,omitempty"`
	Options   string `json:"options,omitempty"`
	Message   string `json:"message"`
}

// FfufArgs for web fuzzing.
type FfufArgs struct {
	URL      string `json:"url"`
	Wordlist string `json:"wordlist"`
	Method   string `json:"method,omitempty"`
	Filters  string `json:"filters,omitempty"`
	Flags    string `json:"flags,omitempty"`
	Message  string `json:"message"`
}

// GobusterArgs for directory/DNS brute-forcing.
type GobusterArgs struct {
	URL      string `json:"url"`
	Wordlist string `json:"wordlist"`
	Mode     string `json:"mode,omitempty"`
	Flags    string `json:"flags,omitempty"`
	Message  string `json:"message"`
}

// WfuzzArgs for web fuzzing.
type WfuzzArgs struct {
	URL      string `json:"url"`
	Wordlist string `json:"wordlist"`
	Param    string `json:"param,omitempty"`
	Filters  string `json:"filters,omitempty"`
	Flags    string `json:"flags,omitempty"`
	Message  string `json:"message"`
}
