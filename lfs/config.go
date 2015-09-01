package lfs

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/github/git-lfs/git"
	"github.com/github/git-lfs/vendor/_nuts/github.com/rubyist/tracerx"
)

type AuthType string

const (
	AuthTypeNone  = AuthType("none")
	AuthTypeBasic = AuthType("basic")
)

var (
	Config        = NewConfig()
	defaultRemote = "origin"
)

// FetchPruneConfig collects together the config options that control fetching and pruning
type FetchPruneConfig struct {
	// The number of days prior to current date for which (local) refs other than HEAD
	// will be fetched with --recent (default 7, 0 = only fetch HEAD)
	FetchRecentRefsDays int
	// Makes the FetchRecentRefsDays option apply to remote refs from fetch source as well (default true)
	FetchRecentRefsIncludeRemotes bool
	// number of days prior to latest commit on a ref that we'll fetch previous
	// LFS changes too (default 0 = only fetch at ref)
	FetchRecentCommitsDays int
	// Whether to always fetch recent even without --recent
	FetchRecentAlways bool
	// Number of days added to FetchRecent*; data outside combined window will be
	// deleted when prune is run. (default 3)
	PruneOffsetDays int
}

type Configuration struct {
	CurrentRemote         string
	httpClient            *HttpClient
	redirectingHttpClient *http.Client
	envVars               map[string]string
	isTracingHttp         bool
	isLoggingStats        bool

	loading           sync.Mutex // guards initialization of gitConfig and remotes
	gitConfig         map[string]string
	remotes           []string
	extensions        map[string]Extension
	fetchIncludePaths []string
	fetchExcludePaths []string
	fetchPruneConfig  *FetchPruneConfig
}

func NewConfig() *Configuration {
	c := &Configuration{
		CurrentRemote: defaultRemote,
		envVars:       make(map[string]string),
	}
	c.isTracingHttp = c.GetenvBool("GIT_CURL_VERBOSE", false)
	c.isLoggingStats = c.GetenvBool("GIT_LOG_STATS", false)
	return c
}

func (c *Configuration) Getenv(key string) string {
	if i, ok := c.envVars[key]; ok {
		return i
	}

	v := os.Getenv(key)
	c.envVars[key] = v
	return v
}

func (c *Configuration) Setenv(key, value string) error {
	// Check see if we have this in our cache, if so update it
	if _, ok := c.envVars[key]; ok {
		c.envVars[key] = value
	}

	// Now set in process
	return os.Setenv(key, value)
}

// GetenvBool parses a boolean environment variable and returns the result as a bool.
// If the environment variable is unset, empty, or if the parsing fails,
// the value of def (default) is returned instead.
func (c *Configuration) GetenvBool(key string, def bool) bool {
	s := c.Getenv(key)
	if len(s) == 0 {
		return def
	}

	b, err := strconv.ParseBool(s)
	if err != nil {
		return def
	}
	return b
}

func (c *Configuration) Endpoint() Endpoint {
	if url, ok := c.GitConfig("lfs.url"); ok {
		return NewEndpoint(url)
	}

	if len(c.CurrentRemote) > 0 && c.CurrentRemote != defaultRemote {
		if endpoint := c.RemoteEndpoint(c.CurrentRemote); len(endpoint.Url) > 0 {
			return endpoint
		}
	}

	return c.RemoteEndpoint(defaultRemote)
}

func (c *Configuration) ConcurrentTransfers() int {
	uploads := 3

	if v, ok := c.GitConfig("lfs.concurrenttransfers"); ok {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			uploads = n
		}
	}

	return uploads
}

func (c *Configuration) BatchTransfer() bool {
	if v, ok := c.GitConfig("lfs.batch"); ok {
		if v == "true" || v == "" {
			return true
		}

		// Any numeric value except 0 is considered true
		n, _ := strconv.Atoi(v)
		return n != 0
	}
	return true
}

// PrivateAccess will retrieve the access value and return true if
// the value is set to private. When a repo is marked as having private
// access, the http requests for the batch api will fetch the credentials
// before running, otherwise the request will run without credentials.
func (c *Configuration) PrivateAccess() bool {
	return c.Access() != AuthTypeNone
}

// Access returns the access auth type.
func (c *Configuration) Access() AuthType {
	return c.EndpointAccess(c.Endpoint())
}

// SetAccess will set the private access flag in .git/config.
func (c *Configuration) SetAccess(authType AuthType) {
	c.SetEndpointAccess(c.Endpoint(), authType)
}

func (c *Configuration) EndpointAccess(e Endpoint) AuthType {
	key := fmt.Sprintf("lfs.%s.access", e.Url)
	if v, ok := c.GitConfig(key); ok && len(v) > 0 {
		return AuthType(strings.ToLower(v))
	}
	return AuthTypeNone
}

func (c *Configuration) SetEndpointAccess(e Endpoint, authType AuthType) {
	tracerx.Printf("setting repository access to %s", authType)
	key := fmt.Sprintf("lfs.%s.access", e.Url)
	configFile := filepath.Join(LocalGitDir, "config")

	// Modify the config cache because it's checked again in this process
	// without being reloaded.
	if authType == AuthTypeNone {
		git.Config.UnsetLocalKey(configFile, key)

		c.loading.Lock()
		delete(c.gitConfig, key)
		c.loading.Unlock()
	} else {
		git.Config.SetLocal(configFile, key, string(authType))

		c.loading.Lock()
		c.gitConfig[key] = string(authType)
		c.loading.Unlock()
	}
}

func (c *Configuration) FetchIncludePaths() []string {
	c.loadGitConfig()
	return c.fetchIncludePaths
}
func (c *Configuration) FetchExcludePaths() []string {
	c.loadGitConfig()
	return c.fetchExcludePaths
}

func (c *Configuration) RemoteEndpoint(remote string) Endpoint {
	if len(remote) == 0 {
		remote = defaultRemote
	}
	if url, ok := c.GitConfig("remote." + remote + ".lfsurl"); ok {
		return NewEndpoint(url)
	}

	if url, ok := c.GitConfig("remote." + remote + ".url"); ok {
		return NewEndpointFromCloneURL(url)
	}

	return Endpoint{}
}

func (c *Configuration) Remotes() []string {
	c.loadGitConfig()
	return c.remotes
}

func (c *Configuration) Extensions() map[string]Extension {
	c.loadGitConfig()
	return c.extensions
}

func (c *Configuration) GitConfig(key string) (string, bool) {
	c.loadGitConfig()
	value, ok := c.gitConfig[strings.ToLower(key)]
	return value, ok
}

func (c *Configuration) SetConfig(key, value string) {
	c.loadGitConfig()
	c.gitConfig[key] = value
}

func (c *Configuration) ObjectUrl(oid string) (*url.URL, error) {
	return ObjectUrl(c.Endpoint(), oid)
}

func (c *Configuration) FetchPruneConfig() *FetchPruneConfig {
	if c.fetchPruneConfig == nil {
		c.fetchPruneConfig = &FetchPruneConfig{
			FetchRecentRefsDays:           7,
			FetchRecentRefsIncludeRemotes: true,
			FetchRecentCommitsDays:        0,
			PruneOffsetDays:               3,
		}
		if v, ok := c.GitConfig("lfs.fetchrecentrefsdays"); ok {
			n, err := strconv.Atoi(v)
			if err == nil && n >= 0 {
				c.fetchPruneConfig.FetchRecentRefsDays = n
			}
		}
		if v, ok := c.GitConfig("lfs.fetchrecentremoterefs"); ok {

			if v == "false" {
				c.fetchPruneConfig.FetchRecentRefsIncludeRemotes = false
			}

			// Any numeric value except 0 is considered true
			if n, err := strconv.Atoi(v); err == nil && n == 0 {
				c.fetchPruneConfig.FetchRecentRefsIncludeRemotes = false
			}
		}
		if v, ok := c.GitConfig("lfs.fetchrecentcommitsdays"); ok {
			n, err := strconv.Atoi(v)
			if err == nil && n >= 0 {
				c.fetchPruneConfig.FetchRecentCommitsDays = n
			}
		}
		if v, ok := c.GitConfig("lfs.fetchrecentalways"); ok {
			if v == "true" || v == "" {
				c.fetchPruneConfig.FetchRecentAlways = true
			}
		}
		if v, ok := c.GitConfig("lfs.pruneoffsetdays"); ok {
			n, err := strconv.Atoi(v)
			if err == nil && n >= 0 {
				c.fetchPruneConfig.PruneOffsetDays = n
			}
		}

	}
	return c.fetchPruneConfig
}

func (c *Configuration) loadGitConfig() {
	c.loading.Lock()
	defer c.loading.Unlock()

	if c.gitConfig != nil {
		return
	}

	uniqRemotes := make(map[string]bool)

	c.gitConfig = make(map[string]string)
	c.extensions = make(map[string]Extension)

	var output string
	listOutput, err := git.Config.List()
	if err != nil {
		panic(fmt.Errorf("Error listing git config: %s", err))
	}

	configFile := filepath.Join(LocalWorkingDir, ".gitconfig")
	fileOutput, err := git.Config.ListFromFile(configFile)
	if err != nil {
		panic(fmt.Errorf("Error listing git config from file: %s", err))
	}

	localConfig := filepath.Join(LocalGitDir, "config")
	localOutput, err := git.Config.ListFromFile(localConfig)
	if err != nil {
		panic(fmt.Errorf("Error listing git config from file %s", err))
	}

	output = fileOutput + "\n" + listOutput + "\n" + localOutput

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		pieces := strings.SplitN(line, "=", 2)
		if len(pieces) < 2 {
			continue
		}
		key := strings.ToLower(pieces[0])
		value := pieces[1]
		c.gitConfig[key] = value

		keyParts := strings.Split(key, ".")
		if len(keyParts) > 1 && keyParts[0] == "remote" {
			remote := keyParts[1]
			uniqRemotes[remote] = remote == "origin"
		} else if len(keyParts) == 4 && keyParts[0] == "lfs" && keyParts[1] == "extension" {
			name := keyParts[2]
			ext := c.extensions[name]
			switch keyParts[3] {
			case "clean":
				ext.Clean = value
			case "smudge":
				ext.Smudge = value
			case "priority":
				p, err := strconv.Atoi(value)
				if err == nil && p >= 0 {
					ext.Priority = p
				}
			}
			ext.Name = name
			c.extensions[name] = ext
		} else if len(keyParts) == 2 && keyParts[0] == "lfs" && keyParts[1] == "fetchinclude" {
			for _, inc := range strings.Split(value, ",") {
				inc = strings.TrimSpace(inc)
				c.fetchIncludePaths = append(c.fetchIncludePaths, inc)
			}
		} else if len(keyParts) == 2 && keyParts[0] == "lfs" && keyParts[1] == "fetchexclude" {
			for _, ex := range strings.Split(value, ",") {
				ex = strings.TrimSpace(ex)
				c.fetchExcludePaths = append(c.fetchExcludePaths, ex)
			}
		}
	}

	c.remotes = make([]string, 0, len(uniqRemotes))
	for remote, isOrigin := range uniqRemotes {
		if isOrigin {
			continue
		}
		c.remotes = append(c.remotes, remote)
	}
}
