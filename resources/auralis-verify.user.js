// ==UserScript==
// @name         Auralis Verification Branding
// @namespace    https://github.com/vekhyat/Auralis
// @version      1.0.0
// @description  Presents the upstream verification pages (Zarz / SpotBye gateways) with Auralis branding. Purely cosmetic — does not touch verification logic.
// @author       Vekhyat Jain
// @match        https://api.zarz.moe/*
// @match        https://verify.spotbye.qzz.io/*
// @run-at       document-start
// @grant        none
// ==/UserScript==

(function () {
	"use strict";

	const STYLE = `
		/* --- Zarz challenge page (api.zarz.moe) ---
		   nav / donate / powered-by / footer are server-rendered branding;
		   the hero (main) holds the Turnstile widget and stays. */
		nav, footer, section, .footer-note { display: none !important; }
		.hero { padding-top: 88px !important; padding-bottom: 72px !important; }

		/* --- SpotBye verify page (verify.spotbye.qzz.io) --- */
		.brand, .tagline, .coffee-button { display: none !important; }

		html { background: #000 !important; }
	`;

	const TITLE_RE = /SpotiFLAC/i;
	const TEXT_RE = /SpotiFLAC(?:-Mobile)?/g;

	function injectStyle() {
		const el = document.createElement("style");
		el.id = "auralis-branding";
		el.textContent = STYLE;
		(document.head || document.documentElement).appendChild(el);
	}

	function cleanTitle() {
		if (document.title && TITLE_RE.test(document.title)) {
			document.title = "Auralis Verification";
		}
	}

	// Rewrite leftover "SpotiFLAC(-Mobile)" strings in visible text,
	// e.g. the fallback hint "paste it into SpotiFLAC-Mobile to finish signing in."
	function scrubText(root) {
		if (!root || !root.childNodes) return;
		const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
			acceptNode(node) {
				return node.nodeValue && node.nodeValue.indexOf("SpotiFLAC") !== -1
					? NodeFilter.FILTER_ACCEPT
					: NodeFilter.FILTER_REJECT;
			},
		});
		const hits = [];
		while (walker.nextNode()) hits.push(walker.currentNode);
		for (const node of hits) {
			node.nodeValue = node.nodeValue.replace(TEXT_RE, "Auralis");
		}
	}

	let scheduled = false;
	const observer = new MutationObserver(() => {
		if (scheduled) return;
		scheduled = true;
		requestAnimationFrame(() => {
			scheduled = false;
			cleanTitle();
			scrubText(document.body);
		});
	});

	injectStyle();
	observer.observe(document, { childList: true, subtree: true });

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", () => {
			cleanTitle();
			scrubText(document.body);
		});
	} else {
		cleanTitle();
		scrubText(document.body);
	}
})();
