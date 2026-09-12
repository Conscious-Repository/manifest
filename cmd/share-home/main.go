// share-home moves Home into shared Markdown, preserving a Home-only backup.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"manifest/goals"
	"manifest/mdfm"
	"manifest/record"
	"manifest/sharedhome"
	"manifest/tasks"
	"manifest/threads"
	"manifest/vaultwriter"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := flag.String("vault", "", "vault root")
	audit := flag.String("audit-dir", "", "audit directory")
	flag.Parse()
	if *root == "" {
		panic("vault required")
	}
	if *audit != "" {
		must(os.MkdirAll(*audit, 0700))
	}
	vw := vaultwriter.New(*root).WithAudit(*audit).Grant(vaultwriter.Capability{Name: "home", Zone: record.ZoneSystem, Pattern: "system/home/**", Actor: vaultwriter.ActorUserAction}, vaultwriter.Capability{Name: "goals", Zone: record.ZoneKnowledge, Pattern: "goals.md", Actor: vaultwriter.ActorUserAction}, vaultwriter.Capability{Name: "tasks", Zone: record.ZoneKnowledge, Pattern: "tasks.md", Actor: vaultwriter.ActorUserAction})
	write := vw.BindAbs("home")
	shared := filepath.Join(*root, "system/home")
	for _, name := range []string{"goals.md", "tasks.md"} {
		dest := filepath.Join(shared, name)
		if _, err := os.Stat(dest); err == nil {
			fmt.Println(name, "already shared")
			continue
		}
		raw, err := os.ReadFile(filepath.Join(*root, name))
		must(err)
		_, section := sharedhome.Split(string(raw))
		if section == "" {
			section = "## Home\n"
		}
		olga, err := os.ReadFile(filepath.Join(*root, "system/olga", name))
		if err != nil && !os.IsNotExist(err) {
			must(err)
		}
		_, other := sharedhome.Split(string(olga))
		if strings.TrimSpace(other) != "" && strings.TrimSpace(other) != "## Home" {
			panic("Olga already has Home content; merge it before bootstrapping")
		}
		must(write(filepath.Join(shared, "import-backup", name), []byte(section+"\n")))
		if name == "goals.md" {
			_, section = sharedhome.Split(goals.Serialize(goals.Parse(section)))
		} else {
			_, section = sharedhome.Split(tasks.Serialize(tasks.Parse(section)))
		}
		must(write(dest, []byte(section+"\n")))
		fmt.Println(name, "Home shared")
	}
	for _, name := range []string{"goals.md", "tasks.md"} {
		p := filepath.Join(*root, name)
		raw, err := os.ReadFile(p)
		must(err)
		private, home := sharedhome.Split(string(raw))
		if home != "" {
			backup, err := os.ReadFile(filepath.Join(shared, "import-backup", name))
			must(err)
			if strings.TrimSpace(home) != strings.TrimSpace(string(backup)) {
				panic("Home changed since import; reconcile before removing private section")
			}
			must(vw.BindAbs(strings.TrimSuffix(name, ".md"))(p, []byte(private)))
		}
	}

	// Carry existing Home descriptions into the shared Markdown notes.
	raw, err := os.ReadFile(filepath.Join(shared, "tasks.md"))
	must(err)
	for _, dom := range tasks.Parse(string(raw)).Domains {
		for _, t := range dom.Tasks {
			slug := record.Slug(strings.NewReplacer(":", "-", "/", "-").Replace(t.ID), 64)
			old, err := os.ReadFile(filepath.Join(*root, "system/todo-plans", slug+".md"))
			if err != nil {
				continue
			}
			_, body := mdfm.Split(string(old))
			start := strings.Index(body, "## description\n")
			if start < 0 {
				continue
			}
			description := body[start+len("## description\n"):]
			if end := strings.Index(description, "\n## plan"); end >= 0 {
				description = description[:end]
			}
			description = strings.TrimSpace(description)
			hash := sha256.Sum256([]byte(t.ID))
			dest := filepath.Join(shared, "notes", hex.EncodeToString(hash[:]), "description.md")
			if _, err := os.Stat(dest); err == nil {
				continue
			}
			must(write(dest, []byte(description)))
			fmt.Println("Home description carried forward")
		}
	}
	var oldThreads struct {
		Comments map[string][]threads.Comment `json:"comments"`
	}
	if b, err := os.ReadFile(filepath.Join(*audit, "todo-threads/threads.json")); err == nil {
		must(json.Unmarshal(b, &oldThreads))
	}
	doc := tasks.Parse(string(raw))
	for id, comments := range oldThreads.Comments {
		if _, t := doc.Find(id); t == nil {
			continue
		}
		hash := sha256.Sum256([]byte(id))
		for _, c := range comments {
			if c.Action != threads.ActComment || c.Author != "owner" || len(c.Files) > 0 {
				continue
			}
			dest := filepath.Join(shared, "notes", hex.EncodeToString(hash[:]), "comment-"+record.Slug(c.ID, 64)+".md")
			if _, err := os.Stat(dest); err == nil {
				continue
			}
			body := c.Text
			c.Text = ""
			metadata, err := json.Marshal(c)
			must(err)
			must(write(dest, []byte("<!-- "+string(metadata)+" -->\n\n"+body)))
			fmt.Println("Home comment carried forward")
		}
	}

}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
