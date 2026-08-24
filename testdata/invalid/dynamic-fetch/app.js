// A comment mentioning fetch("./decoy.json") must not be reported.
var note = "we no longer fetch('./old.json')";

fetch("./presentation.json").then(function (r) { return r.json(); });

var worker = new Worker("./worker.js");
