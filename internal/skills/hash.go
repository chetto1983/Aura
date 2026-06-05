package skills

// CanonicalHash is Aura's OWN canonical skill-content pin (D-15): the SHA-256 over
// the byte-sorted (relPath, content) pairs of every regular file under dir, with
// symlinks excluded (Lstat-no-follow). It is the install TOFU pin written into
// skills-lock.json and recorded on the audit row — deliberately NOT interoperable
// with the upstream skills-lock.json `computedHash`, which is locale/platform-
// sensitive (autocrlf / LF-blob rewrites, spike 004b). Two installs of the same
// post-checkout tree produce an identical CanonicalHash regardless of FS walk
// order or platform.
//
// It is a thin formalization over the established HashSkillDir (contenthash.go,
// 11-04): the writer (single-file SKILL.md) and the installer (cloned tree) share
// ONE sha256 hashing helper so a create-then-reinstall of identical content matches.
// The "sha256:" prefix is part of the pin (the algorithm is named in the value).
func CanonicalHash(dir string) (string, error) {
	return HashSkillDir(dir)
}

// canonicalHashAlgo names the digest the canonical pin uses; referenced by the
// installer's lockfile writer so the algorithm identifier is single-sourced.
const canonicalHashAlgo = "sha256"
