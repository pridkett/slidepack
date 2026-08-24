package validate

import (
	"errors"

	"github.com/pwagstro/slidepack/internal/diag"
	"github.com/pwagstro/slidepack/internal/envelope"
	"github.com/pwagstro/slidepack/internal/unpack"
)

// Packed validates a packed presentation end to end: envelope, manifest,
// format version, base64, payload digest, gzip, tar, manifest/archive
// agreement, per-file digests, and finally the recovered source tree against
// the same rules a directory would face.
//
// It stops at the first structural failure. Once base64 decoding has failed
// there is nothing meaningful to say about the archive inside, and a pile of
// cascading errors would obscure the one that matters.
func Packed(data []byte, target string) *diag.Result {
	res := diag.NewResult(target, "package")

	pkg, err := unpack.Open(data, unpack.Options{})
	if err != nil {
		var ue *unpack.Error
		if errors.As(err, &ue) {
			res.Errorf(ue.Code, ue.Path, 0, "", "%s", ue.Error())
		} else {
			res.Errorf(diag.MalformedEnvelope, "", 0, "", "%v", err)
		}
		return res
	}

	// Cross-check the declared payload attributes against the manifest, so a
	// hand-edited envelope that claims a different encoding is caught.
	if doc, derr := envelope.Parse(data); derr == nil {
		checkAttr(res, doc.PayloadAttrs, "data-format", pkg.Manifest.Payload.Archive)
		checkAttr(res, doc.PayloadAttrs, "data-compression", pkg.Manifest.Payload.Compression)
		checkAttr(res, doc.PayloadAttrs, "data-encoding", pkg.Manifest.Payload.Encoding)
	}

	sub := Tree(pkg.Tree(), Options{Entrypoint: pkg.Manifest.Entrypoint})
	res.Merge(sub)
	res.Sort()
	return res
}

func checkAttr(res *diag.Result, attrs map[string]string, name, want string) {
	got, ok := attrs[name]
	if !ok {
		res.Warnf(diag.MalformedEnvelope, "", 0, name, "the payload element does not declare %s", name)
		return
	}
	if got != want {
		res.Errorf(diag.MalformedEnvelope, "", 0, name,
			"the payload element declares %s=%q but the manifest says %q", name, got, want)
	}
}
