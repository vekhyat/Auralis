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
	const AURALIS_ICON = "data:image/svg+xml," + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="#f1f2f4"/><rect x="7" y="7" width="50" height="50" fill="none" stroke="#262b33" stroke-width="2"/><rect x="18" y="21" width="17" height="2.5" fill="#262b33"/><rect x="15" y="30.75" width="34" height="2.5" fill="#27506f"/><rect x="21" y="40.5" width="22" height="2.5" fill="#262b33"/></svg>');

	function replaceFavicon() {
		const head = document.head || document.documentElement;
		document.querySelectorAll('link[rel*="icon"], link[rel="apple-touch-icon"]').forEach((node) => {
			if (node.id !== "auralis-favicon") node.remove();
		});
		let icon = document.getElementById("auralis-favicon");
		if (!icon) {
			icon = document.createElement("link");
			icon.id = "auralis-favicon";
			icon.rel = "icon";
			icon.type = "image/svg+xml";
			head.appendChild(icon);
		}
		icon.setAttribute("href", AURALIS_ICON);
	}

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
			replaceFavicon();
			cleanTitle();
			scrubText(document.body);
		});
	});

	injectStyle();
	replaceFavicon();
	observer.observe(document, { childList: true, subtree: true });

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", () => {
			replaceFavicon();
			cleanTitle();
			scrubText(document.body);
		});
	} else {
		replaceFavicon();
		cleanTitle();
		scrubText(document.body);
	}
})();
