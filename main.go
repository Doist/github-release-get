// Command github-release-get downloads single asset from the latest published
// release of given github repository.
//
// It downloads first asset matching given pattern of the latest published
// github release to the current directory; it stops if file with such name
// already exists. For pattern matching see https://golang.org/pkg/path/#Match
//
// To access private repositories pass oAuth token via GITHUB_TOKEN environment
// variable, see https://github.com/settings/tokens page.
package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/artyom/autoflags"
	"github.com/google/go-github/v25/github"
	"golang.org/x/oauth2"
)

func main() {
	args := runArgs{
		Timeout: time.Minute,
		Token:   os.Getenv("GITHUB_TOKEN"),
	}
	autoflags.Parse(&args)
	if err := run(context.Background(), args); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

type runArgs struct {
	Owner   string `flag:"owner,repository owner (user or org name)"`
	Repo    string `flag:"repo,repository name"`
	Pattern string `flag:"pattern,pattern to match release asset name"`

	Timeout time.Duration `flag:"timeout"`

	Token string // filled in from environment
}

func run(ctx context.Context, args runArgs) error {
	if args.Owner == "" || args.Repo == "" || args.Pattern == "" {
		return fmt.Errorf("one or more mandatory flags missing")
	}
	if args.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, args.Timeout)
		defer cancel()
	}
	var client *github.Client
	switch args.Token {
	case "":
		client = github.NewClient(nil)
	default:
		client = github.NewClient(oauth2.NewClient(ctx,
			oauth2.StaticTokenSource(&oauth2.Token{AccessToken: args.Token})))
	}
	release, _, err := client.Repositories.GetLatestRelease(ctx, args.Owner, args.Repo)
	if err != nil {
		return err
	}
	var id int64
	var name string
	for _, asset := range release.Assets {
		name = asset.GetName()
		ok, err := path.Match(args.Pattern, name)
		if err != nil {
			return err
		}
		if ok {
			id = asset.GetID()
			name = asset.GetName()
			break
		}
	}
	if name == "" {
		return fmt.Errorf("empty asset name")
	}
	dst := filepath.Base(filepath.FromSlash(name))
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		return fmt.Errorf("file %q already exists", dst)
	}
	if id == 0 {
		names := make([]string, 0, len(release.Assets))
		for _, asset := range release.Assets {
			names = append(names, asset.GetName())
		}
		return fmt.Errorf("no assets matching pattern %q found, assets are: %v", args.Pattern, names)
	}
	rc, u, err := client.Repositories.DownloadReleaseAsset(ctx, args.Owner, args.Repo, id)
	if err != nil {
		return err
	}
	tf, err := ioutil.TempFile("", ".github-release-asset-*")
	if err != nil {
		return err
	}
	defer tf.Close()
	defer os.Remove(tf.Name())
	switch {
	case rc != nil:
		defer rc.Close()
		if _, err := io.Copy(tf, rc); err != nil {
			return err
		}
	case u != "":
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req = req.WithContext(ctx)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			return fmt.Errorf("invalid status: %s", r.Status)
		}
		if _, err := io.Copy(tf, r.Body); err != nil {
			return err
		}
	default:
		return fmt.Errorf("cannot download asset release, don't have sensible link for that")
	}
	if err := tf.Close(); err != nil {
		return err
	}
	return os.Rename(tf.Name(), dst)
}
