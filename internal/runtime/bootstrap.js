/*
 * slidepack format v1 browser runtime.
 *
 * Reads the base64/gzip/tar payload embedded in this document, expands it in
 * memory, rewrites package-local references to blob: URLs, and renders the
 * entrypoint in a full-viewport srcdoc iframe.
 *
 * Nothing is written to disk. No network request is made. There is no eval()
 * and no new Function(); the presentation's own scripts run because the
 * browser loads them normally inside the frame, not because this code
 * evaluates them.
 *
 * The only global this file defines is window.slidepack, a small read-only
 * diagnostics object.
 */
(function () {
  "use strict";

  var FORMAT = "slidepack";
  var VERSION = 1;
  var MANIFEST_ID = "slidepack-manifest";
  var PAYLOAD_ID = "slidepack-payload";
  var STATUS_ID = "slidepack-status";
  var FRAME_ID = "slidepack-frame";
  var XLINK_NS = "http://www.w3.org/1999/xlink";

  var diagnostics = { stage: "start", warnings: [] };

  /* ---------------------------------------------------------------------
   * User-visible status
   * ------------------------------------------------------------------ */

  function statusEl() {
    return document.getElementById(STATUS_ID);
  }

  function warn(message) {
    diagnostics.warnings.push(message);
    if (window.console && console.warn) {
      console.warn("[slidepack] " + message);
    }
  }

  /**
   * Replaces the loading indicator with a visible, readable failure panel.
   * The full error (including its stack) goes to the console; the panel shows
   * only a one-line reason, because a stack trace on screen tells the person
   * looking at a broken slide deck nothing they can act on.
   */
  function showError(reason, cause) {
    var host = statusEl();
    if (window.console && console.error) {
      console.error("[slidepack] failed during stage '" + diagnostics.stage + "': " + reason);
      if (cause) console.error(cause);
    }
    diagnostics.error = reason;
    if (!host) return;
    host.hidden = false;
    host.textContent = "";

    var panel = document.createElement("div");
    panel.className = "slidepack-panel";

    var h = document.createElement("p");
    h.className = "slidepack-title";
    h.textContent = "This presentation could not be loaded.";
    panel.appendChild(h);

    var label = document.createElement("p");
    label.className = "slidepack-label";
    label.textContent = "Reason";
    panel.appendChild(label);

    var pre = document.createElement("p");
    pre.className = "slidepack-reason";
    pre.textContent = reason;
    panel.appendChild(pre);

    var foot = document.createElement("p");
    foot.className = "slidepack-footnote";
    foot.textContent =
      "This is a slidepack format v1 presentation. The file may be truncated or " +
      "corrupted; try obtaining a fresh copy. Full diagnostics are in the browser console.";
    panel.appendChild(foot);

    host.appendChild(panel);
    var frame = document.getElementById(FRAME_ID);
    if (frame && frame.parentNode) frame.parentNode.removeChild(frame);
  }

  /* ---------------------------------------------------------------------
   * Base64
   * ------------------------------------------------------------------ */

  /**
   * Decodes a base64 string into a Uint8Array without ever materialising the
   * whole decoded payload as an intermediate JavaScript string.
   *
   * atob() on a 40 MB string would allocate a 30 MB binary string on top of
   * the 40 MB source and the 30 MB result. Decoding in 256 KiB slices keeps
   * the transient allocation bounded while the destination array is written
   * once, in place.
   */
  function decodeBase64(text) {
    var s = text.trim();
    if (s.length === 0) throw new Error("the embedded payload is empty");
    if (s.length % 4 !== 0) {
      throw new Error("the embedded payload is not valid base64 (length is not a multiple of 4)");
    }
    var pad = 0;
    if (s.charCodeAt(s.length - 1) === 61) pad++;
    if (s.charCodeAt(s.length - 2) === 61) pad++;
    var out = new Uint8Array((s.length / 4) * 3 - pad);

    var CHUNK = 262144; /* multiple of 4 */
    var o = 0;
    for (var i = 0; i < s.length; i += CHUNK) {
      var bin;
      try {
        bin = atob(s.slice(i, i + CHUNK));
      } catch (e) {
        throw new Error("the embedded payload is not valid base64 (bad character near offset " + i + ")");
      }
      for (var j = 0; j < bin.length; j++) {
        out[o++] = bin.charCodeAt(j) & 0xff;
      }
    }
    if (o !== out.length) {
      throw new Error("base64 decoding produced " + o + " bytes, expected " + out.length);
    }
    return out;
  }

  function toHex(buffer) {
    var view = new Uint8Array(buffer);
    var hex = "";
    for (var i = 0; i < view.length; i++) {
      hex += (view[i] < 16 ? "0" : "") + view[i].toString(16);
    }
    return hex;
  }

  /* ---------------------------------------------------------------------
   * TAR
   * ------------------------------------------------------------------ */

  var BLOCK = 512;
  var utf8 = new TextDecoder("utf-8");

  function tarString(bytes, off, len) {
    var end = off;
    var limit = off + len;
    while (end < limit && bytes[end] !== 0) end++;
    return utf8.decode(bytes.subarray(off, end));
  }

  function tarOctal(bytes, off, len) {
    var s = "";
    for (var i = off; i < off + len; i++) {
      var c = bytes[i];
      if (c === 0 || c === 32) continue;
      s += String.fromCharCode(c);
    }
    if (s === "") return 0;
    var v = parseInt(s, 8);
    if (isNaN(v)) throw new Error("corrupt archive: malformed octal field");
    return v;
  }

  function isZeroBlock(bytes, off) {
    for (var i = off; i < off + BLOCK; i++) {
      if (bytes[i] !== 0) return false;
    }
    return true;
  }

  /**
   * Parses the USTAR subset slidepack emits: regular files and (optionally)
   * directory records, nothing else.
   *
   * Strictness is deliberate. This archive is never user-authored, so any
   * deviation means corruption or tampering, and reporting that is far more
   * useful than silently skipping a record.
   */
  function readTar(bytes) {
    var files = [];
    var off = 0;
    while (off + BLOCK <= bytes.length) {
      if (isZeroBlock(bytes, off)) break;

      if (
        bytes[off + 257] !== 117 || /* u */
        bytes[off + 258] !== 115 || /* s */
        bytes[off + 259] !== 116 || /* t */
        bytes[off + 260] !== 97 ||  /* a */
        bytes[off + 261] !== 114    /* r */
      ) {
        throw new Error("corrupt archive: record at offset " + off + " is not a USTAR header");
      }

      var stored = tarOctal(bytes, off + 148, 8);
      var unsigned = 0;
      var signed = 0;
      for (var i = 0; i < BLOCK; i++) {
        var c = i >= 148 && i < 156 ? 32 : bytes[off + i];
        unsigned += c;
        signed += c > 127 ? c - 256 : c;
      }
      if (stored !== unsigned && stored !== signed) {
        throw new Error("corrupt archive: header checksum mismatch at offset " + off);
      }

      var name = tarString(bytes, off, 100);
      var prefix = tarString(bytes, off + 345, 155);
      var path = prefix ? prefix + "/" + name : name;
      var mode = tarOctal(bytes, off + 100, 8);
      var size = tarOctal(bytes, off + 124, 12);
      var type = bytes[off + 156];
      if (type === 0) type = 48; /* historical NUL means regular file */

      off += BLOCK;

      if (type === 53 /* '5' directory */) {
        continue;
      }
      if (type !== 48 /* '0' regular */) {
        throw new Error(
          "corrupt archive: entry '" + path + "' has unsupported type flag '" + String.fromCharCode(type) + "'"
        );
      }
      if (off + size > bytes.length) {
        throw new Error("corrupt archive: entry '" + path + "' claims " + size + " bytes but the archive ends first");
      }
      files.push({ path: path, mode: mode, data: bytes.subarray(off, off + size) });
      off += size;
      if (size % BLOCK !== 0) off += BLOCK - (size % BLOCK);
    }
    if (files.length === 0) {
      throw new Error("the archive contains no files");
    }
    return files;
  }

  /* ---------------------------------------------------------------------
   * Path handling (mirrors internal/pathutil in the Go tool)
   * ------------------------------------------------------------------ */

  /**
   * Cleans a slash-separated path. Returns null when a relative path climbs
   * above the package root; root-relative paths clamp at the root instead,
   * which is what a browser does with "/../x".
   */
  function normalizePath(p, rootRelative) {
    var parts = p.split("/");
    var out = [];
    for (var i = 0; i < parts.length; i++) {
      var seg = parts[i];
      if (seg === "" || seg === ".") continue;
      if (seg === "..") {
        if (out.length === 0) {
          if (rootRelative) continue;
          return null;
        }
        out.pop();
        continue;
      }
      out.push(seg);
    }
    if (out.length === 0) return null;
    return out.join("/");
  }

  function resolvePath(base, ref) {
    if (ref.charAt(0) === "/") {
      return normalizePath(ref.slice(1), true);
    }
    var slash = base.lastIndexOf("/");
    var dir = slash >= 0 ? base.slice(0, slash) : "";
    return normalizePath(dir ? dir + "/" + ref : ref, false);
  }

  var IGNORABLE_SCHEMES = {
    data: 1, blob: 1, javascript: 1, mailto: 1, tel: 1, sms: 1, about: 1, geo: 1, cid: 1
  };

  function schemeOf(s) {
    for (var i = 0; i < s.length; i++) {
      var c = s.charAt(i);
      if (c === ":") return i === 0 ? null : s.slice(0, i).toLowerCase();
      var ok =
        (c >= "a" && c <= "z") || (c >= "A" && c <= "Z") ||
        (i > 0 && ((c >= "0" && c <= "9") || c === "+" || c === "." || c === "-"));
      if (!ok) return null;
    }
    return null;
  }

  /** Splits a raw reference into the parts the resolver needs. */
  function classifyRef(raw) {
    var s = String(raw).replace(/[\n\r\t]/g, "").trim();
    if (s === "") return { cls: "empty" };
    if (s.charAt(0) === "#") return { cls: "ignorable" };
    if (s.slice(0, 2) === "//") return { cls: "remote" };
    var scheme = schemeOf(s);
    if (scheme) {
      return { cls: IGNORABLE_SCHEMES[scheme] ? "ignorable" : "remote" };
    }
    var frag = "";
    var hash = s.indexOf("#");
    if (hash >= 0) {
      frag = s.slice(hash + 1);
      s = s.slice(0, hash);
    }
    var q = s.indexOf("?");
    if (q >= 0) s = s.slice(0, q);
    if (s === "") return { cls: "ignorable" };
    var decoded = s;
    try {
      decoded = decodeURIComponent(s);
    } catch (e) {
      /* Leave the raw spelling in place; some file names contain literal %. */
    }
    return { cls: "local", path: decoded, rawPath: s, fragment: frag };
  }

  /* ---------------------------------------------------------------------
   * CSS scanner (mirrors internal/source/css.go)
   * ------------------------------------------------------------------ */

  function isCSSSpace(c) {
    return c === " " || c === "\t" || c === "\n" || c === "\r" || c === "\f";
  }

  function isIdentChar(c) {
    if (c === undefined || c === "") return false;
    return c === "-" || c === "_" || (c >= "0" && c <= "9") ||
      (c >= "a" && c <= "z") || (c >= "A" && c <= "Z") || c.charCodeAt(0) >= 0x80;
  }

  function readCSSString(src, i) {
    var quote = src.charAt(i);
    i++;
    var buf = "";
    while (i < src.length) {
      var ch = src.charAt(i);
      if (ch === "\\" && i + 1 < src.length) {
        if (src.charAt(i + 1) === "\n") { i += 2; continue; }
        buf += src.charAt(i + 1);
        i += 2;
        continue;
      }
      if (ch === quote) return { value: buf, end: i + 1 };
      if (ch === "\n") return { value: buf, end: i };
      buf += ch;
      i++;
    }
    return { value: buf, end: src.length };
  }

  function readURLToken(src, i) {
    while (i < src.length && isCSSSpace(src.charAt(i))) i++;
    if (i >= src.length) return null;
    var ch = src.charAt(i);
    if (ch === '"' || ch === "'") {
      var start = i + 1;
      var r = readCSSString(src, i);
      var end = r.end;
      var valueEnd = end - 1;
      while (end < src.length && isCSSSpace(src.charAt(end))) end++;
      if (src.charAt(end) === ")") end++;
      return { ref: { value: r.value, start: start, end: valueEnd }, next: end };
    }
    var vStart = i;
    var buf = "";
    while (i < src.length) {
      var c = src.charAt(i);
      if (c === ")" || isCSSSpace(c)) break;
      if (c === "\\" && i + 1 < src.length) { buf += src.charAt(i + 1); i += 2; continue; }
      buf += c;
      i++;
    }
    var vEnd = i;
    while (i < src.length && isCSSSpace(src.charAt(i))) i++;
    if (src.charAt(i) === ")") i++;
    return { ref: { value: buf, start: vStart, end: vEnd }, next: i };
  }

  /**
   * Finds every url() token and @import target in a stylesheet.
   *
   * A three-state scan (default / string / comment) rather than a regular
   * expression, because `content: "url(x)"`, `/* url(x) *\/` and
   * `url(data:...)` must not be mistaken for package references, and any
   * pattern loose enough to catch every real form would also corrupt string
   * literals.
   */
  function scanCSS(src) {
    var refs = [];
    var i = 0;
    var n = src.length;
    var pendingImport = false;

    while (i < n) {
      var c = src.charAt(i);
      if (c === "/" && src.charAt(i + 1) === "*") {
        var close = src.indexOf("*/", i + 2);
        i = close < 0 ? n : close + 2;
        continue;
      }
      if (c === '"' || c === "'") {
        var start = i + 1;
        var s = readCSSString(src, i);
        if (pendingImport) {
          refs.push({ value: s.value, start: start, end: s.end - 1, isImport: true });
          pendingImport = false;
        }
        i = s.end;
        continue;
      }
      if (c === "@") {
        if (src.substr(i, 7).toLowerCase() === "@import" && !isIdentChar(src.charAt(i + 7))) {
          pendingImport = true;
          i += 7;
          continue;
        }
        pendingImport = false;
        i += 1;
        continue;
      }
      if ((c === "u" || c === "U") && src.substr(i, 3).toLowerCase() === "url") {
        var prev = i > 0 ? src.charAt(i - 1) : "";
        if (isIdentChar(prev) || prev === "\\") { i += 3; continue; }
        var j = i + 3;
        while (j < n && isCSSSpace(src.charAt(j))) j++;
        if (src.charAt(j) !== "(") { i += 3; continue; }
        var tok = readURLToken(src, j + 1);
        if (!tok) { i += 3; continue; }
        tok.ref.isImport = pendingImport;
        pendingImport = false;
        refs.push(tok.ref);
        i = tok.next;
        continue;
      }
      if (c === ";" || c === "{" || c === "}") {
        pendingImport = false;
      }
      i++;
    }
    return refs;
  }

  /** Parses a srcset attribute into candidate spans. */
  function parseSrcset(attr) {
    var out = [];
    var i = 0;
    var n = attr.length;
    while (i < n) {
      while (i < n && (isCSSSpace(attr.charAt(i)) || attr.charAt(i) === ",")) i++;
      if (i >= n) break;
      var start = i;
      while (i < n && !isCSSSpace(attr.charAt(i))) i++;
      var candidate = attr.slice(start, i);
      var trimmed = candidate.replace(/,+$/, "");
      var hadComma = trimmed.length !== candidate.length;
      if (trimmed !== "") out.push({ url: trimmed, start: start, end: start + trimmed.length });
      if (hadComma) continue;
      while (i < n && attr.charAt(i) !== ",") i++;
    }
    return out;
  }

  /* ---------------------------------------------------------------------
   * Virtual filesystem
   * ------------------------------------------------------------------ */

  function makeVFS(files, manifest) {
    var byPath = new Map();
    var mimeByPath = new Map();
    if (manifest && manifest.files) {
      for (var i = 0; i < manifest.files.length; i++) {
        var mf = manifest.files[i];
        mimeByPath.set(mf.path, mf.mime);
      }
    }
    for (var k = 0; k < files.length; k++) {
      var f = files[k];
      byPath.set(f.path, {
        bytes: f.data,
        mime: mimeByPath.get(f.path) || guessMime(f.path)
      });
    }
    return byPath;
  }

  var MIME_BY_EXT = {
    html: "text/html; charset=utf-8", htm: "text/html; charset=utf-8",
    css: "text/css; charset=utf-8",
    js: "text/javascript; charset=utf-8", mjs: "text/javascript; charset=utf-8",
    json: "application/json", map: "application/json",
    svg: "image/svg+xml", png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg",
    gif: "image/gif", webp: "image/webp", avif: "image/avif", bmp: "image/bmp",
    ico: "image/x-icon",
    woff: "font/woff", woff2: "font/woff2", ttf: "font/ttf", otf: "font/otf",
    mp3: "audio/mpeg", wav: "audio/wav", ogg: "audio/ogg", m4a: "audio/mp4",
    mp4: "video/mp4", webm: "video/webm", mov: "video/quicktime",
    vtt: "text/vtt; charset=utf-8",
    txt: "text/plain; charset=utf-8", xml: "application/xml", pdf: "application/pdf",
    wasm: "application/wasm"
  };

  function extOf(p) {
    var dot = p.lastIndexOf(".");
    var slash = p.lastIndexOf("/");
    if (dot < 0 || dot < slash) return "";
    return p.slice(dot + 1).toLowerCase();
  }

  function guessMime(p) {
    return MIME_BY_EXT[extOf(p)] || "application/octet-stream";
  }

  /* ---------------------------------------------------------------------
   * Rewriting
   * ------------------------------------------------------------------ */

  function makeResolver(vfs) {
    var urls = new Map();
    var cssInProgress = new Set();
    var encoder = new TextEncoder();

    /** Returns (creating if needed) the blob URL for a package path. */
    function urlFor(path) {
      if (urls.has(path)) return urls.get(path);
      var entry = vfs.get(path);
      if (!entry) return null;

      var bytes = entry.bytes;
      if (extOf(path) === "css") {
        if (cssInProgress.has(path)) {
          // Browsers ignore recursive @import too; breaking the cycle here
          // keeps a pathological stylesheet from hanging the loader.
          warn("ignoring circular @import involving '" + path + "'");
          return null;
        }
        cssInProgress.add(path);
        try {
          bytes = encoder.encode(rewriteCSS(utf8.decode(entry.bytes), path));
        } finally {
          cssInProgress.delete(path);
        }
      }
      var url = URL.createObjectURL(new Blob([bytes], { type: entry.mime }));
      urls.set(path, url);
      return url;
    }

    /**
     * Resolves one raw reference found in `base` to a blob URL, or null when
     * the reference is not a package resource.
     */
    function refToURL(base, raw) {
      var c = classifyRef(raw);
      if (c.cls !== "local") return null;
      var target = resolvePath(base, c.path);
      var url = target ? urlFor(target) : null;
      if (url === null && c.rawPath !== c.path) {
        var alt = resolvePath(base, c.rawPath);
        if (alt) url = urlFor(alt);
      }
      if (url === null) {
        warn("unresolved reference '" + raw + "' in '" + base + "'");
        return null;
      }
      return c.fragment ? url + "#" + c.fragment : url;
    }

    /** Splices blob URLs into a stylesheet without re-serialising it. */
    function rewriteCSS(text, base) {
      var refs = scanCSS(text);
      if (refs.length === 0) return text;
      var parts = [];
      var last = 0;
      for (var i = 0; i < refs.length; i++) {
        var r = refs[i];
        var url = refToURL(base, r.value);
        if (url === null) continue;
        parts.push(text.slice(last, r.start));
        parts.push(url);
        last = r.end;
      }
      parts.push(text.slice(last));
      return parts.join("");
    }

    function rewriteSrcset(value, base) {
      var cands = parseSrcset(value);
      if (cands.length === 0) return value;
      var parts = [];
      var last = 0;
      for (var i = 0; i < cands.length; i++) {
        var c = cands[i];
        var url = refToURL(base, c.url);
        if (url === null) continue;
        parts.push(value.slice(last, c.start));
        parts.push(url);
        last = c.end;
      }
      parts.push(value.slice(last));
      return parts.join("");
    }

    return {
      urlFor: urlFor,
      refToURL: refToURL,
      rewriteCSS: rewriteCSS,
      rewriteSrcset: rewriteSrcset,
      count: function () { return urls.size; }
    };
  }

  /* Element/attribute pairs whose value names a subresource. */
  var URL_ATTRS = {
    script: ["src"],
    link: ["href"],
    img: ["src"],
    source: ["src"],
    video: ["src", "poster"],
    audio: ["src"],
    track: ["src"],
    object: ["data"],
    embed: ["src"],
    input: ["src"],
    image: ["href"],
    use: ["href"],
    body: ["background"],
    table: ["background"],
    td: ["background"],
    th: ["background"]
  };

  var SRCSET_ATTRS = { img: ["srcset"], source: ["srcset"], link: ["imagesrcset"] };

  /* rel values that make link[href] a loaded resource rather than metadata. */
  var LINK_RESOURCE_RELS = {
    stylesheet: 1, icon: 1, shortcut: 1, "apple-touch-icon": 1,
    "apple-touch-startup-image": 1, "mask-icon": 1, preload: 1, prefetch: 1,
    manifest: 1, prerender: 1
  };

  function linkIsResource(el) {
    var rel = (el.getAttribute("rel") || "").toLowerCase().split(/\s+/);
    for (var i = 0; i < rel.length; i++) {
      if (LINK_RESOURCE_RELS[rel[i]]) return true;
    }
    return false;
  }

  /**
   * Rewrites every package-local reference in the entrypoint document.
   *
   * The document comes from DOMParser, which builds a tree without running a
   * single script or fetching a single subresource, so this is a pure text
   * transformation even though it uses DOM APIs.
   */
  function rewriteDocument(doc, base, resolver) {
    var all = doc.querySelectorAll("*");
    for (var i = 0; i < all.length; i++) {
      var el = all[i];
      var tag = el.localName;

      var attrs = URL_ATTRS[tag];
      if (attrs) {
        for (var a = 0; a < attrs.length; a++) {
          var name = attrs[a];
          if (tag === "link" && name === "href" && !linkIsResource(el)) continue;
          if (!el.hasAttribute(name)) continue;
          var url = resolver.refToURL(base, el.getAttribute(name));
          if (url !== null) el.setAttribute(name, url);
        }
      }

      /* SVG's legacy xlink:href, still emitted by many drawing tools. */
      if (tag === "image" || tag === "use") {
        var xl = el.getAttributeNS(XLINK_NS, "href");
        if (xl) {
          var xurl = resolver.refToURL(base, xl);
          if (xurl !== null) el.setAttributeNS(XLINK_NS, "xlink:href", xurl);
        }
      }

      var srcsets = SRCSET_ATTRS[tag];
      if (srcsets) {
        for (var s = 0; s < srcsets.length; s++) {
          if (!el.hasAttribute(srcsets[s])) continue;
          el.setAttribute(srcsets[s], resolver.rewriteSrcset(el.getAttribute(srcsets[s]), base));
        }
      }

      if (tag === "style") {
        el.textContent = resolver.rewriteCSS(el.textContent, base);
      }

      if (el.hasAttribute("style")) {
        el.setAttribute("style", resolver.rewriteCSS(el.getAttribute("style"), base));
      }
    }

    /* An author <base> would re-anchor relative URLs unpredictably. The
       validator rejects it at pack time; strip it here so a hand-edited file
       degrades predictably rather than mysteriously. */
    var bases = doc.getElementsByTagName("base");
    while (bases.length > 0) {
      warn("removed a <base> element, which is unsupported in format v1");
      bases[0].parentNode.removeChild(bases[0]);
    }

    /*
     * Anchor the frame's base URL to its own document.
     *
     * A srcdoc document inherits its base URL from the parent, so <a href="#x">
     * would resolve against the packed file's own file:// URL. Following such a
     * link navigates the frame to the packed file and reloads the whole
     * presentation from the top -- which is exactly what happened before this
     * was added. Pointing the base at about:srcdoc makes "#x" a same-document
     * fragment navigation again, so hash links scroll and fire hashchange the
     * way they do on an ordinary page.
     *
     * This is safe only because every package-local reference has already been
     * rewritten to an absolute blob: URL by the time we get here; there is no
     * relative resource URL left for the base to affect.
     */
    var head = doc.head || doc.documentElement;
    var base = doc.createElement("base");
    base.setAttribute("href", "about:srcdoc");
    head.insertBefore(base, head.firstChild);
  }

  function serializeDocument(doc) {
    var prefix = "<!DOCTYPE html>\n";
    if (doc.doctype) {
      var dt = doc.doctype;
      prefix = "<!DOCTYPE " + dt.name +
        (dt.publicId ? ' PUBLIC "' + dt.publicId + '"' : "") +
        (!dt.publicId && dt.systemId ? " SYSTEM" : "") +
        (dt.systemId ? ' "' + dt.systemId + '"' : "") + ">\n";
    }
    return prefix + doc.documentElement.outerHTML;
  }

  /* ---------------------------------------------------------------------
   * Main
   * ------------------------------------------------------------------ */

  function readEmbedded(id, what) {
    var node = document.getElementById(id);
    if (!node) {
      throw new Error("this document has no " + what + " element (id=\"" + id + "\"); it may not be a slidepack file");
    }
    return node.textContent || "";
  }

  function parseManifest() {
    var text = readEmbedded(MANIFEST_ID, "manifest");
    var m;
    try {
      m = JSON.parse(text);
    } catch (e) {
      throw new Error("the embedded manifest is not valid JSON");
    }
    if (m.format !== FORMAT) {
      throw new Error("the embedded manifest is not a slidepack manifest");
    }
    if (m.version !== VERSION) {
      throw new Error(
        "this file uses slidepack format version " + m.version +
        ", but this runtime understands version " + VERSION
      );
    }
    if (!m.entrypoint) throw new Error("the manifest names no entrypoint");
    return m;
  }

  async function verifyPayload(bytes, expected) {
    if (!expected || !window.crypto || !window.crypto.subtle) {
      warn("SubtleCrypto is unavailable; skipping the payload integrity check");
      return;
    }
    var digest;
    try {
      digest = await crypto.subtle.digest("SHA-256", bytes);
    } catch (e) {
      warn("payload integrity check could not run: " + e);
      return;
    }
    var got = toHex(digest);
    if (got !== expected) {
      throw new Error(
        "payload integrity check failed: the archive hashes to " + got.slice(0, 16) +
        "… but the manifest expects " + expected.slice(0, 16) + "… (the file is corrupted or truncated)"
      );
    }
  }

  async function decompress(bytes) {
    if (typeof DecompressionStream !== "function") {
      throw new Error(
        "this browser does not support DecompressionStream, which slidepack needs to expand the payload; " +
        "use a current version of Firefox, Chrome, Edge or Safari"
      );
    }
    var stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"));
    var buf;
    try {
      buf = await new Response(stream).arrayBuffer();
    } catch (e) {
      throw new Error("the compressed payload could not be expanded; the file is corrupted or truncated");
    }
    return new Uint8Array(buf);
  }

  function mount(html, title) {
    var frame = document.createElement("iframe");
    frame.id = FRAME_ID;
    frame.setAttribute("title", title || "Presentation");
    /* No sandbox attribute: the presentation's own scripts must run, and the
       frame must stay same-origin with this document so the blob: URLs minted
       above remain readable. */
    frame.setAttribute("allow", "fullscreen; autoplay; clipboard-read; clipboard-write; encrypted-media");
    frame.setAttribute("allowfullscreen", "");

    frame.addEventListener("load", function () {
      var status = statusEl();
      if (status) status.hidden = true;
      try {
        frame.contentWindow.focus();
      } catch (e) {
        /* Focusing is a nicety; never let it break rendering. */
      }
      diagnostics.stage = "ready";
    });

    document.body.appendChild(frame);
    frame.srcdoc = html;
  }

  async function boot() {
    diagnostics.stage = "manifest";
    var manifest = parseManifest();
    diagnostics.entrypoint = manifest.entrypoint;

    diagnostics.stage = "base64";
    var payloadText = readEmbedded(PAYLOAD_ID, "payload");
    var gz = decodeBase64(payloadText);

    diagnostics.stage = "integrity";
    await verifyPayload(gz, manifest.payload && manifest.payload.sha256);

    diagnostics.stage = "decompress";
    var tar = await decompress(gz);
    gz = null;

    diagnostics.stage = "archive";
    var files = readTar(tar);
    var vfs = makeVFS(files, manifest);
    diagnostics.fileCount = files.length;

    var entry = vfs.get(manifest.entrypoint);
    if (!entry) {
      throw new Error("the archive does not contain the entrypoint '" + manifest.entrypoint + "'");
    }

    diagnostics.stage = "rewrite";
    var resolver = makeResolver(vfs);
    var doc = new DOMParser().parseFromString(utf8.decode(entry.bytes), "text/html");
    rewriteDocument(doc, manifest.entrypoint, resolver);

    var title = (doc.title || "").trim();
    if (title) document.title = title;

    diagnostics.stage = "mount";
    mount(serializeDocument(doc), title);
  }

  Object.defineProperty(window, "slidepack", {
    value: Object.freeze({
      version: VERSION,
      diagnostics: diagnostics
    }),
    writable: false,
    configurable: false
  });

  function start() {
    boot().catch(function (err) {
      showError(err && err.message ? err.message : String(err), err);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
