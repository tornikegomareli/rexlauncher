package skills

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// askTimeout bounds an agent that does not answer, so the tab says so.
const askTimeout = 15 * time.Second

// ask starts a second copy of an agent, sends it a request, and reads its output until
// answer accepts a line. The copy reads the agent's own settings for the pane's
// folder, and nothing it does reaches the agent in the pane.
func ask(p Program, name string, args []string, request string, answer func([]byte) bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
	defer cancel()

	// The program can be a script, and its interpreter is then the one Rex names.
	// Either way the folder holds it, and the plugin's PATH can lack that folder.
	folder, path := filepath.Dir(p.Path), name
	if _, err := os.Stat(filepath.Join(folder, name)); err == nil {
		path = filepath.Join(folder, name)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), "PATH="+folder+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Dir = p.Cwd
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	io.WriteString(stdin, request) // If the copy has stopped, the end of its output says so.
	reader := bufio.NewReader(stdout)
	answered := false
	for !answered {
		line, err := reader.ReadBytes('\n')
		answered = answer(line)
		if err != nil {
			break
		}
	}
	cancel() // The copy waits for requests until it is stopped.
	cmd.Wait()
	if !answered {
		return fmt.Errorf("%s did not list its skills: %s", name, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func codexSkills(p Program) ([]Skill, error) {
	cwd, _ := json.Marshal([]string{p.Cwd})
	request := `{"id":0,"method":"initialize","params":{"clientInfo":{"name":"rexlauncher","title":"Rex Launcher","version":"0"}}}
{"method":"initialized"}
{"id":1,"method":"skills/list","params":{"cwds":` + string(cwd) + `}}
`
	var reply struct {
		ID     *int `json:"id"`
		Result struct {
			Data []struct {
				Skills []struct {
					Name        string  `json:"name"`
					Description string  `json:"description"`
					Path        string  `json:"path"`
					Scope       string  `json:"scope"`
					Enabled     bool    `json:"enabled"`
					PluginID    *string `json:"pluginId"`
				} `json:"skills"`
			} `json:"data"`
		} `json:"result"`
	}
	err := ask(p, "codex", []string{"app-server"}, request, func(line []byte) bool {
		return json.Unmarshal(line, &reply) == nil && reply.ID != nil && *reply.ID == 1
	})
	if err != nil {
		return nil, err
	}
	var found []Skill
	for _, data := range reply.Result.Data {
		for _, s := range data.Skills {
			if !s.Enabled {
				continue
			}
			source := map[string]string{"user": "Personal", "repo": "Project", "system": "Built in", "admin": "Admin"}[s.Scope]
			if s.PluginID != nil {
				plugin, _, _ := strings.Cut(*s.PluginID, "@")
				source = "Plugin " + plugin
			}
			found = append(found, Skill{Name: "$" + s.Name, Description: oneLine(s.Description), Source: source, Path: s.Path})
		}
	}
	addFields(found)
	return found, nil
}

func piSkills(p Program) ([]Skill, error) {
	var reply struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Data    struct {
			Commands []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Source      string `json:"source"`
				SourceInfo  struct {
					Path  string `json:"path"`
					Scope string `json:"scope"`
				} `json:"sourceInfo"`
			} `json:"commands"`
		} `json:"data"`
	}
	// Without extensions: one that fails to load stops pi, and only extensions add commands
	// that are not skills.
	err := ask(p, "pi", []string{"-ne", "--mode", "rpc", "--no-session"}, `{"type":"get_commands"}`+"\n", func(line []byte) bool {
		return json.Unmarshal(line, &reply) == nil && reply.Command == "get_commands"
	})
	if err != nil {
		return nil, err
	}
	var found []Skill
	for _, c := range reply.Data.Commands {
		if c.Source != "skill" {
			continue
		}
		source := "Personal"
		if c.SourceInfo.Scope == "project" {
			source = "Project"
		}
		found = append(found, Skill{Name: "/" + c.Name, Description: oneLine(c.Description), Source: source, Path: c.SourceInfo.Path})
	}
	addFields(found)
	return found, nil
}
