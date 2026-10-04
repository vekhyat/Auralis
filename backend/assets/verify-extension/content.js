(function () {
	"use strict";

	if (globalThis.__auralisVerifyBrandingInstalled) {
		return;
	}
	globalThis.__auralisVerifyBrandingInstalled = true;

	const STYLE = `
		html, body {
			background: #f1f2f4 !important;
			color: #262b33 !important;
			font-family: "Segoe UI", system-ui, sans-serif !important;
			margin: 0 !important;
			min-height: 0 !important;
			height: auto !important;
		}
		img[alt*="SpotiFLAC" i], img[src*="spotiflac" i] {
			display: none !important;
		}
		.hero, main, .content, .container, body > div {
			padding: 12px 16px !important;
			margin: 0 !important;
			min-height: 0 !important;
			height: auto !important;
		}
		h1, h2, p, label, .prompt, .subtitle {
			font-family: "Segoe UI", system-ui, sans-serif !important;
			color: #262b33 !important;
		}
		h1, h2 {
			font-size: 13px !important;
			font-weight: 600 !important;
			letter-spacing: 0 !important;
			margin: 0 0 8px !important;
		}
		p, label, .prompt, .subtitle {
			font-size: 12px !important;
			line-height: 1.4 !important;
			color: #5c6370 !important;
			margin: 0 0 10px !important;
		}
		iframe[src*="challenges.cloudflare.com"],
		.cf-turnstile, #cf-turnstile, [id*="turnstile"], [class*="turnstile"] {
			display: block !important;
			visibility: visible !important;
			opacity: 1 !important;
			max-width: 100% !important;
		}
	`;

	const TEXT_RE = /SpotiFLAC(?:-Mobile)?/gi;
	const WIDGET = "iframe[src*='challenges.cloudflare'], .cf-turnstile, #cf-turnstile, [id*='turnstile'], [class*='turnstile']";

	function injectStyle() {
		let el = document.getElementById("auralis-branding");
		if (!el) {
			el = document.createElement("style");
			el.id = "auralis-branding";
			(document.head || document.documentElement).appendChild(el);
		}
		if (el.textContent !== STYLE) {
			el.textContent = STYLE;
		}
	}

	function clearHiddenDisplay(el) {
		if (!el || !el.style) {
			return;
		}
		if (el.style.getPropertyValue("display") === "none") {
			el.style.removeProperty("display");
		}
	}

	function keepForCaptcha(el) {
		if (!el) {
			return false;
		}
		const tag = (el.tagName || "").toLowerCase();
		if (tag === "html" || tag === "body" || tag === "main") {
			return true;
		}
		if (el.classList && el.classList.contains("hero")) {
			return true;
		}
		if (el.matches && el.matches(WIDGET)) {
			return true;
		}
		return Boolean(el.querySelector && el.querySelector(WIDGET));
	}

	function revealWidgetAncestors() {
		document.querySelectorAll(WIDGET).forEach((widget) => {
			let node = widget;
			while (node && node.nodeType === 1) {
				clearHiddenDisplay(node);
				node = node.parentElement;
			}
		});
	}

	function hideChrome() {
		const selectors = "nav, header, footer, aside, .footer-note, .brand, .tagline, .coffee-button, a[href*='buymeacoffee'], a[href*='ko-fi'], a[href*='bmc.xyz'], [class*='donate'], [class*='powered']";
		document.querySelectorAll(selectors).forEach((el) => {
			if (keepForCaptcha(el)) {
				clearHiddenDisplay(el);
				return;
			}
			el.style.setProperty("display", "none", "important");
		});
		revealWidgetAncestors();
	}

	const AURALIS_ICON =
		"data:image/svg+xml," +
		encodeURIComponent(
			'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="#f1f2f4"/><rect x="7" y="7" width="50" height="50" fill="none" stroke="#262b33" stroke-width="2"/><rect x="18" y="21" width="17" height="2.5" fill="#262b33"/><rect x="15" y="30.75" width="34" height="2.5" fill="#27506f"/><rect x="21" y="40.5" width="22" height="2.5" fill="#262b33"/></svg>'
		);

	function replaceFavicon() {
		const head = document.head || document.documentElement;
		document.querySelectorAll('link[rel*="icon"], link[rel="apple-touch-icon"]').forEach((node) => {
			if (node.id !== "auralis-favicon") {
				node.remove();
			}
		});
		let icon = document.getElementById("auralis-favicon");
		if (!icon) {
			icon = document.createElement("link");
			icon.id = "auralis-favicon";
			icon.rel = "icon";
			icon.type = "image/svg+xml";
			head.appendChild(icon);
		}
		if (icon.getAttribute("href") !== AURALIS_ICON) {
			icon.setAttribute("href", AURALIS_ICON);
		}
	}

	function cleanTitle() {
		if (!document.title || /SpotiFLAC/i.test(document.title)) {
			document.title = "Auralis";
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
			if (keepForCaptcha(el)) {
				clearHiddenDisplay(el);
				return;
			}
			if (el.childElementCount > 0) {
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
		replaceFavicon();
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
	observer.observe(document.documentElement, { childList: true, subtree: true });

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", apply);
	} else {
		apply();
	}
})();
