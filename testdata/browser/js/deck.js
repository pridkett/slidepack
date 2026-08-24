/* Ordinary classic browser script: no modules, no dynamic resource loading. */
(function () {
  "use strict";

  var slides = Array.prototype.slice.call(document.querySelectorAll(".slide"));
  var index = 0;

  function show(i) {
    if (i < 0 || i >= slides.length) return;
    index = i;
    var el = slides[i];
    // Fragment navigation is the presentation's own business; slidepack does
    // not intercept it.
    if (location.hash !== "#" + el.id) {
      location.hash = el.id;
    }
    el.scrollIntoView();
    var status = document.getElementById("status");
    if (status) {
      status.textContent = "slide " + (i + 1) + " of " + slides.length;
    }
    document.body.setAttribute("data-slide", el.id);
  }

  function indexOfHash() {
    var id = location.hash.replace(/^#/, "");
    for (var i = 0; i < slides.length; i++) {
      if (slides[i].id === id) return i;
    }
    return -1;
  }

  document.addEventListener("keydown", function (e) {
    if (e.key === "ArrowRight" || e.key === "PageDown" || e.key === " ") {
      e.preventDefault();
      show(index + 1);
    } else if (e.key === "ArrowLeft" || e.key === "PageUp") {
      e.preventDefault();
      show(index - 1);
    }
  });

  window.addEventListener("hashchange", function () {
    var i = indexOfHash();
    if (i >= 0 && i !== index) show(i);
  });

  // The browser test asserts on this marker to prove the script executed.
  window.__deck = {
    ready: true,
    slideCount: slides.length,
    current: function () { return slides[index].id; }
  };

  document.body.setAttribute("data-deck-ready", "true");
  var start = indexOfHash();
  show(start >= 0 ? start : 0);
})();
