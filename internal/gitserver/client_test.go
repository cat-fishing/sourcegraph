	"os/exec"
	"github.com/google/go-cmp/cmp"
	root, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	srv := httptest.NewServer((&server.Server{
		ReposDir: filepath.Join(root, "repos"),
	}).Handler())
	tests := map[api.RepoName]struct {
		remote string
		want   map[string]string
		err    error
		"simple": {
			remote: createSimpleGitRepo(t, root),
		"repo-with-dotgit-dir": {
			remote: createRepoWithDotGitDir(t, root),
			want:   map[string]string{"file1": "hello\n", ".git/mydir/file2": "milton\n", ".git/mydir/": "", ".git/": ""},
		"not-found": {
			err: errors.New("repository does not exist: not-found"),
	for name, test := range tests {
		t.Run(string(name), func(t *testing.T) {
			if test.remote != "" {
				if _, err := cli.RequestRepoUpdate(ctx, gitserver.Repo{Name: name, URL: test.remote}, 0); err != nil {
					t.Fatal(err)
				}
			}
			rc, err := cli.Archive(ctx, gitserver.Repo{Name: name}, gitserver.ArchiveOptions{Treeish: "HEAD", Format: "zip"})
			if have, want := fmt.Sprint(err), fmt.Sprint(test.err); have != want {
				t.Errorf("archive: have err %v, want %v", have, want)
			}
			if rc == nil {
				return
			}
			defer rc.Close()
			data, err := ioutil.ReadAll(rc)
				t.Fatal(err)
			zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				t.Fatal(err)
			got := map[string]string{}
			for _, f := range zr.File {
				r, err := f.Open()
				if err != nil {
					t.Errorf("failed to open %q because %s", f.Name, err)
					continue
				}
				contents, err := ioutil.ReadAll(r)
				_ = r.Close()
				if err != nil {
					t.Errorf("Read(%q): %s", f.Name, err)
					continue
				}
				got[f.Name] = string(contents)
			}

			if !cmp.Equal(test.want, got) {
				t.Errorf("mismatch (-want +got):\n%s", cmp.Diff(test.want, got))
			}
		})
func createRepoWithDotGitDir(t *testing.T, root string) string {
	t.Helper()
		t.Helper()
			t.Fatal(err)
	dir := filepath.Join(root, "remotes", "repo-with-dot-git-dir")

			t.Fatal(err)
			t.Fatal(err)
		}
	}
	return dir
}

func createSimpleGitRepo(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "remotes", "simple")

	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range []string{
		"git init",
		"mkdir dir1",
		"echo -n infile1 > dir1/file1",
		"touch --date=2006-01-02T15:04:05Z dir1 dir1/file1 || touch -t 200601021704.05 dir1 dir1/file1",
		"git add dir1/file1",
		"GIT_COMMITTER_NAME=a GIT_COMMITTER_EMAIL=a@a.com GIT_COMMITTER_DATE=2006-01-02T15:04:05Z git commit -m commit1 --author='a <a@a.com>' --date 2006-01-02T15:04:05Z",
		"echo -n infile2 > 'file 2'",
		"touch --date=2014-05-06T19:20:21Z 'file 2' || touch -t 201405062120.21 'file 2'",
		"git add 'file 2'",
		"GIT_COMMITTER_NAME=a GIT_COMMITTER_EMAIL=a@a.com GIT_COMMITTER_DATE=2014-05-06T19:20:21Z git commit -m commit2 --author='a <a@a.com>' --date 2014-05-06T19:20:21Z",
	} {
		c := exec.Command("bash", "-c", cmd)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("Command %q failed. Output was:\n\n%s", cmd, out)

	return dir