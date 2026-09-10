(function () {
	"use strict";

	const STYLE = `
		html, body {
			background: #000 !important;
			color: #f5f5f5 !important;
		}
		nav, header, footer, aside,
		.footer-note, .brand, .tagline, .coffee-button,
		a[href*="buymeacoffee"], a[href*="ko-fi"], a[href*="bmc.xyz"],
		[class*="donate"], [class*="powered"], [class*="spotiflac" i] {
			display: none !important;
		}
		img[alt*="SpotiFLAC" i], img[src*="spotiflac" i] {
			display: none !important;
		}
		.hero {
			padding-top: 48px !important;
			padding-bottom: 48px !important;
		}
		iframe[src*="challenges.cloudflare.com"],
		.cf-turnstile, #cf-turnstile, [id*="turnstile"], [class*="turnstile"] {
			display: block !important;
			visibility: visible !important;
		}
	`;

	const TEXT_RE = /SpotiFLAC(?:-Mobile)?/gi;

	function injectStyle() {
		let el = document.getElementById("auralis-branding");
		if (!el) {
			el = document.createElement("style");
			el.id = "auralis-branding";
			(document.head || document.documentElement).appendChild(el);
		}
		el.textContent = STYLE;
	}

	function keepForCaptcha(el) {
		if (!el || !el.querySelector) {
			return false;
		}
		return Boolean(
			el.querySelector("iframe[src*='challenges.cloudflare'], .cf-turnstile, #cf-turnstile, [id*='turnstile'], [class*='turnstile']")
		);
	}

	function hideChrome() {
		const selectors = "nav, header, footer, aside, section, .footer-note, .brand, .tagline, .coffee-button";
		document.querySelectorAll(selectors).forEach((el) => {
			if (keepForCaptcha(el)) {
				return;
			}
			el.style.setProperty("display", "none", "important");
		});
	}

	function cleanTitle() {
		if (document.title && /SpotiFLAC/i.test(document.title)) {
			document.title = "Verification";
		}
	}

	function scrubText(root) {
		if (!root || !root.childNodes) {
			return;
		}
		const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
			acceptNode(node) {
				return node.nodeValue && /SpotiFLAC/i.test(node.nodeValue)
					? NodeFilter.FILTER_ACCEPT
					: NodeFilter.FILTER_REJECT;
			},
		});
		const hits = [];
		while (walker.nextNode()) {
			hits.push(walker.currentNode);
		}
		for (const node of hits) {
			node.nodeValue = node.nodeValue.replace(TEXT_RE, "").replace(/\s{2,}/g, " ");
		}
	}

	function hideBrandingCopy() {
		document.querySelectorAll("p, span, div, a, h1, h2, h3, li, small").forEach((el) => {
			if (el.childElementCount > 0 || keepForCaptcha(el)) {
				return;
			}
			const text = (el.textContent || "").trim();
			if (/SpotiFLAC|Powered by this API|Buy Me a Coffee|Buy me a coffee/i.test(text)) {
				el.style.setProperty("display", "none", "important");
			}
		});
	}

	function apply() {
		injectStyle();
		hideChrome();
		hideBrandingCopy();
		cleanTitle();
		if (document.body) {
			scrubText(document.body);
		}
	}

	let scheduled = false;
	const observer = new MutationObserver(() => {
		if (scheduled) {
			return;
		}
		scheduled = true;
		requestAnimationFrame(() => {
			scheduled = false;
			apply();
		});
	});

	injectStyle();
	observer.observe(document.documentElement, { childList: true, subtree: true, characterData: true });

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", apply);
	} else {
		apply();
	}
})();
