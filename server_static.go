package skillsdex

import (
	"io"
	"net/http"
)

func (a *app) handleCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, css)
}

const css = `
:root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #1f252c; background: #eceff1; --bg: #eceff1; --bg-grid: rgba(44,56,65,.08); --surface: #ffffff; --surface-2: #f7f8f7; --surface-3: #eef3f2; --line: #d6dde2; --text: #1f252c; --muted: #66717a; --accent: #1f6f78; --accent-strong: #174f56; --accent-ink: #0f3439; --clay: #a05a3c; --clay-soft: #fff1ea; --gold: #a06f10; --danger: #a33838; --ok: #237a4d; --warn: #9a6a16; --nav: rgba(255,255,255,.88); --nav-link: #334c57; --nav-hover: #e9f3f4; --row-hover: #f8fbfb; --field: #ffffff; --field-border: #c4cdd4; --soft: #f1f5f4; --soft-border: #dce5e4; --code-bg: #101820; --code-text: #f6f8fa; --shadow: rgba(30,39,46,.10); --shadow-soft: rgba(30,39,46,.05); --focus: rgba(31,111,120,.24); --warning-bg: #fff7e6; --warning-line: #ecd39a; --warning-text: #6f4c0b; --ok-bg: #eef8f2; --ok-line: #cce8d7; }
:root[data-theme="dark"] { color-scheme: dark; color: #e8edf1; background: #101418; --bg: #101418; --bg-grid: rgba(209,220,224,.07); --surface: #171d23; --surface-2: #1e252c; --surface-3: #202c30; --line: #303a43; --text: #e8edf1; --muted: #9da9b3; --accent: #44a7ad; --accent-strong: #86d4d9; --accent-ink: #c9f2f3; --clay: #e49a7b; --clay-soft: #2c211d; --gold: #e0bd62; --danger: #ff8b8b; --ok: #79d49c; --warn: #e6bd6a; --nav: rgba(20,26,31,.90); --nav-link: #c5d7da; --nav-hover: #213137; --row-hover: #1c252b; --field: #10161b; --field-border: #3a4650; --soft: #202a31; --soft-border: #35424b; --code-bg: #090e13; --code-text: #e8eef5; --shadow: rgba(0,0,0,.30); --shadow-soft: rgba(0,0,0,.18); --focus: rgba(68,167,173,.32); --warning-bg: #332716; --warning-line: #6b5224; --warning-text: #f0cf86; --ok-bg: #173025; --ok-line: #2f6048; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; background: var(--bg); background-image: linear-gradient(var(--bg-grid) 1px, transparent 1px), linear-gradient(90deg, var(--bg-grid) 1px, transparent 1px); background-size: 28px 28px; }
.app-shell { min-height: 100vh; }
nav { min-height: 66px; display: flex; align-items: center; gap: 18px; padding: 0 28px; background: var(--nav); border-bottom: 1px solid var(--line); position: sticky; top: 0; z-index: 10; backdrop-filter: blur(14px); box-shadow: 0 1px 0 rgba(255,255,255,.35); }
nav a { color: var(--nav-link); text-decoration: none; font-weight: 700; }
nav a:hover { color: var(--accent-strong); }
nav form { margin-left: auto; }
.nav-links { display: flex; align-items: center; gap: 6px; }
.nav-links a { display: inline-flex; align-items: center; gap: 7px; min-height: 36px; padding: 8px 10px; border-radius: 7px; }
.nav-links a:hover { background: var(--nav-hover); }
.nav-links a[aria-current="page"] { background: var(--accent-ink); color: #fff; }
:root[data-theme="dark"] .nav-links a[aria-current="page"] { background: var(--accent); color: #071114; }
.brand { color: var(--text); font-size: 18px; display: inline-flex; align-items: center; gap: 10px; margin-right: 6px; }
.brand-mark, .mark { display: inline-grid; place-items: center; width: 36px; height: 36px; border-radius: 8px; background: var(--accent-ink); color: #fff; font-weight: 850; letter-spacing: 0; box-shadow: inset 0 0 0 1px rgba(255,255,255,.16), 0 8px 18px var(--shadow-soft); }
main { max-width: 1260px; margin: 0 auto; padding: 32px; }
h1 { font-size: 32px; line-height: 1.12; margin: 0 0 10px; letter-spacing: 0; }
h2 { font-size: 18px; margin: 0 0 14px; letter-spacing: 0; }
p { line-height: 1.55; }
a { color: var(--accent-strong); }
table { width: 100%; border-collapse: collapse; background: var(--surface); }
th, td { text-align: left; padding: 15px 16px; border-bottom: 1px solid var(--line); vertical-align: middle; }
th { font-size: 11px; text-transform: uppercase; color: var(--muted); background: var(--surface-2); letter-spacing: .04em; }
tr:hover td { background: var(--row-hover); }
tbody tr:last-child td { border-bottom: 0; }
code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
pre { white-space: pre-wrap; overflow: auto; }
.surface, .panel, .rendered { background: var(--surface); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 12px 32px var(--shadow-soft), 0 1px 2px var(--shadow); overflow: hidden; }
.panel { padding: 22px; overflow: visible; }
.data-surface { border-top: 3px solid var(--accent); }
.narrow { max-width: 480px; margin: 42px auto; }
.split { display: grid; grid-template-columns: minmax(0, 1fr) 360px; gap: 22px; align-items: start; }
.split.wide { grid-template-columns: minmax(0, 1fr) 380px; }
.sticky { position: sticky; top: 84px; }
.pagehead { display: flex; justify-content: space-between; gap: 18px; align-items: flex-start; margin-bottom: 24px; padding: 24px; background: color-mix(in srgb, var(--surface) 86%, transparent); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 10px 28px var(--shadow-soft); overflow: hidden; position: relative; }
.pagehead p { margin: 0; color: var(--muted); max-width: 720px; }
.eyebrow { font-size: 12px; font-weight: 800; text-transform: uppercase; color: var(--accent-strong) !important; letter-spacing: .08em; margin-bottom: 6px !important; }
.metrics { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 12px; }
.metrics span { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 5px 10px; border-radius: 7px; background: var(--soft); border: 1px solid var(--soft-border); color: var(--muted); font-size: 13px; }
.metrics strong { color: var(--text); }
form { margin: 0; }
label { display: grid; gap: 7px; margin-bottom: 15px; font-weight: 700; color: var(--text); }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
input, textarea { width: 100%; border: 1px solid var(--field-border); border-radius: 7px; padding: 11px 12px; font: inherit; background: var(--field); color: var(--text); box-shadow: inset 0 1px 0 var(--shadow-soft); }
input:focus, textarea:focus, button:focus-visible, a:focus-visible { outline: 3px solid var(--focus); outline-offset: 2px; border-color: var(--accent); }
textarea { resize: vertical; }
button, .button { display: inline-flex; align-items: center; justify-content: center; gap: 8px; min-height: 38px; border: 1px solid var(--accent-strong); border-radius: 7px; background: var(--accent); color: #fff; padding: 8px 14px; font: inherit; font-weight: 800; text-decoration: none; cursor: pointer; box-shadow: 0 1px 0 rgba(255,255,255,.16) inset; }
button:hover, .button:hover { background: var(--accent-strong); color: #fff; }
.secondary { background: var(--surface); color: var(--accent-strong); }
.secondary:hover { background: var(--nav-hover); color: var(--accent-strong); }
.ghost { background: transparent; color: var(--muted); border-color: var(--line); box-shadow: none; }
.ghost:hover { background: var(--soft); color: var(--text); }
.link { border: 0; background: transparent; color: var(--nav-link); padding: 0; min-height: 0; font-weight: 750; box-shadow: none; }
.link:hover { background: transparent; color: var(--accent-strong); }
.danger { color: var(--danger); }
.check { display: flex; align-items: center; gap: 8px; }
.check input { width: auto; }
.actions { display: flex; gap: 10px; flex-wrap: wrap; }
.theme-toggle { margin-left: auto; min-height: 34px; padding: 6px 10px; background: var(--surface); color: var(--text); border-color: var(--line); }
.theme-toggle:hover { background: var(--nav-hover); color: var(--text); }
.theme-toggle + form { margin-left: 0; }
.nav-logout { display: inline-flex; align-items: center; gap: 7px; }
.flash { background: var(--warning-bg); border: 1px solid var(--warning-line); padding: 11px 13px; border-radius: 6px; color: var(--warning-text); }
.flash.success { background: var(--ok-bg); border-color: var(--ok-line); color: var(--ok); }
.setup-list { display: grid; gap: 14px; }
.setup-step { display: grid; grid-template-columns: 42px minmax(0, 1fr); gap: 14px; }
.setup-step-index { width: 34px; height: 34px; border-radius: 999px; display: inline-grid; place-items: center; background: var(--accent-ink); color: #fff; font-weight: 850; }
.setup-step-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 6px; }
	.setup-step h2, .setup-step p { margin: 0; }
	.setup-step p { color: var(--muted); }
	.setup-step .button { margin-top: 12px; }
	.membership-list { display: grid; gap: 8px; margin: 18px 0; }
	.membership-row { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: start; gap: 10px; margin: 0; padding: 10px; border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); }
	.membership-row input { width: auto; margin-top: 3px; }
	.membership-row span { display: grid; gap: 3px; min-width: 0; }
	.membership-row em { color: var(--muted); font-style: normal; font-size: 13px; overflow-wrap: anywhere; }
	.search-block { margin-top: 24px; padding-top: 20px; border-top: 1px solid var(--line); }
.inline-search { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; align-items: end; margin-bottom: 14px; }
.inline-search label { margin-bottom: 0; }
.help-intent-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 14px; margin-bottom: 22px; }
.help-principle { display: grid; gap: 8px; }
.help-principle span, .help-facts dt { color: var(--muted); font-size: 11px; font-weight: 800; letter-spacing: .06em; text-transform: uppercase; }
.help-principle strong { font-size: 17px; }
.help-principle p { margin: 0; color: var(--muted); }
.help-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 16px; }
.help-card { display: grid; gap: 14px; }
.help-card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.help-card-head h2 { margin: 0; }
.help-card-head a { white-space: nowrap; font-size: 13px; font-weight: 800; }
.help-summary { margin: 0; color: var(--muted); }
.help-facts { display: grid; gap: 12px; margin: 0; }
.help-facts div { display: grid; gap: 5px; padding-top: 12px; border-top: 1px solid var(--line); }
.help-facts dd { margin: 0; line-height: 1.45; }
.help-grid { margin-top: 22px; }
.note-list { display: grid; gap: 12px; }
.note-list p { margin: 0; }
.path-list { display: grid; gap: 2px; }
code { background: var(--soft); border: 1px solid var(--soft-border); border-radius: 4px; padding: 1px 4px; }
.sort-head { min-height: 0; padding: 0; border: 0; background: transparent; color: inherit; font: inherit; font-weight: inherit; text-transform: inherit; letter-spacing: inherit; }
.sort-head:hover { background: transparent; color: var(--accent-strong); }
.sort-head::after { content: "sort"; margin-left: 5px; color: var(--muted); font-size: 9px; text-transform: none; }
.sort-head[aria-sort="ascending"]::after { content: "asc"; color: var(--accent-strong); }
.sort-head[aria-sort="descending"]::after { content: "desc"; color: var(--accent-strong); }
.editor { display: grid; grid-template-columns: minmax(0, 1fr) minmax(360px, 500px); gap: 22px; align-items: start; }
.preview-pane { max-height: calc(100vh - 112px); overflow: auto; }
.editor-assist { display: flex; flex-wrap: wrap; gap: 8px; margin: -6px 0 12px; color: var(--muted); font-size: 12px; font-weight: 800; }
.editor-assist span { display: inline-flex; min-height: 26px; align-items: center; padding: 3px 8px; border: 1px solid var(--soft-border); border-radius: 999px; background: var(--soft); }
.template-picks { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin: 0 0 15px; padding: 10px; border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); }
.template-picks span { color: var(--muted); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .06em; }
.template-picks a { font-weight: 800; text-decoration: none; }
.support-editor { display: grid; gap: 12px; margin: 18px 0; padding-top: 18px; border-top: 1px solid var(--line); }
.support-file-editor { display: grid; gap: 10px; padding: 12px; border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); }
.support-file-editor label { margin-bottom: 0; }
.support-file-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.support-file-head strong { min-width: 0; overflow-wrap: anywhere; }
.check.compact { margin-bottom: 0; font-size: 13px; color: var(--muted); }
.quality-section { margin-bottom: 18px; }
.quality-head { margin-bottom: 12px; }
.quality-list { display: grid; gap: 10px; margin-bottom: 18px; }
.quality-item { border: 1px solid var(--soft-border); border-left-width: 4px; border-radius: 7px; padding: 11px 12px; background: var(--soft); }
.quality-item.warn { border-left-color: var(--warn); background: var(--warning-bg); }
.quality-item.info { border-left-color: var(--accent); }
.quality-item strong { display: block; margin-bottom: 4px; }
.quality-item p { margin: 0; color: var(--muted); font-size: 13px; }
.quality-ready { color: var(--ok); background: var(--ok-bg); border-color: var(--ok-line); }
.quality-needs-attention { color: var(--warn); background: var(--warning-bg); border-color: var(--warning-line); }
.raw { background: var(--code-bg); color: var(--code-text); border-radius: 6px; padding: 14px; border: 1px solid #263241; }
.rendered { padding: 28px; overflow: visible; line-height: 1.62; }
.rendered h1:first-child { margin-top: 0; }
.mini { padding: 14px; }
.muted { color: var(--muted); font-size: 13px; margin-top: 4px; }
.row-tags { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.tag-filter { display: flex; flex-wrap: wrap; gap: 8px; margin: -10px 0 22px; }
.tag-filter a { text-decoration: none; }
.identifier { font-weight: 800; text-decoration: none; }
.pill, .rev { display: inline-flex; align-items: center; min-height: 24px; padding: 3px 8px; border-radius: 999px; border: 1px solid var(--soft-border); background: var(--soft); color: var(--text); font-size: 12px; font-weight: 800; }
.pill.active { color: var(--ok); background: var(--ok-bg); border-color: var(--ok-line); }
.pill.pinned { color: var(--gold); background: var(--warning-bg); border-color: var(--warning-line); }
	.pill.source { color: var(--accent-strong); background: var(--surface-3); border-color: var(--soft-border); }
	.pill.archived, .pill.revoked, .pill.deactivated { color: var(--warn); background: var(--warning-bg); border-color: var(--warning-line); }
.token-cell code { display: block; max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.invite-table { table-layout: fixed; }
.invite-table th, .invite-table td { vertical-align: middle; }
.invite-table th:nth-child(1) { width: 37%; }
.invite-table th:nth-child(2) { width: 12%; }
.invite-table th:nth-child(3), .invite-table th:nth-child(4) { width: 15%; }
.invite-table th:nth-child(5) { width: 9%; }
.invite-table th:nth-child(6) { width: 12%; }
.invite-token-cell .muted { margin-top: 5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.invite-date-cell { white-space: nowrap; font-variant-numeric: tabular-nums; }
.invite-status-cell, .invite-used-cell { white-space: nowrap; }
.invite-action-cell { text-align: right; white-space: nowrap; }
.invite-action-cell form { display: inline-flex; margin: 0; }
	.one-time-token { display: grid; gap: 8px; margin: 16px; padding: 14px; border-radius: 7px; border: 1px solid var(--ok-line); background: var(--ok-bg); }
	.one-time-token span { color: var(--ok); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .06em; }
	.one-time-token code { display: block; overflow-wrap: anywhere; background: var(--surface); }
	.empty { padding: 32px; text-align: center; color: var(--muted); }
.empty.compact { padding: 12px; text-align: left; border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); margin-bottom: 18px; }
.command-card { display: grid; gap: 8px; margin-top: 14px; }
.command-card span { color: var(--muted); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .06em; }
.command-card pre { margin: 0; background: var(--code-bg); color: var(--code-text); border-radius: 7px; padding: 13px; border: 1px solid #263241; }
.meta-list { display: grid; gap: 12px; margin-top: 18px; }
.meta-list div { display: grid; gap: 4px; padding-top: 12px; border-top: 1px solid var(--line); }
.meta-list span { color: var(--muted); font-size: 12px; text-transform: uppercase; font-weight: 800; letter-spacing: .06em; }
.skill-install-panel { margin-bottom: 18px; }
.skill-install-panel .command-card { max-width: 620px; }
.detail-meta { grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 14px; }
.detail-meta div { min-width: 0; }
.detail-meta strong { overflow-wrap: anywhere; }
.skill-body { margin-top: 18px; }
.file-list, .package-diffs { display: grid; gap: 10px; }
.file-item, .package-diff { border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); overflow: hidden; }
.file-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 38px; padding: 9px 10px; }
.file-row code { min-width: 0; overflow-wrap: anywhere; }
.file-row span { flex: 0 0 auto; color: var(--muted); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .04em; }
.file-preview { margin: 0; max-height: 240px; overflow: auto; border-top: 1px solid var(--soft-border); border-radius: 0; padding: 10px; background: var(--surface); color: var(--text); }
.package-diff .diff { border: 0; border-top: 1px solid var(--soft-border); border-radius: 0; margin: 0; }
.icon { display: inline-flex; align-items: center; justify-content: center; width: 17px; height: 17px; flex: 0 0 auto; }
.icon svg { width: 17px; height: 17px; fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
.panel-head { display: flex; gap: 12px; align-items: flex-start; margin-bottom: 18px; }
.panel-head.compact { margin-bottom: 12px; }
.panel-head h2, .panel-head p { margin: 0; }
.panel-head p { color: var(--muted); font-size: 13px; }
.panel-icon { width: 38px; height: 38px; border-radius: 8px; display: inline-grid; place-items: center; color: var(--accent-strong); background: var(--surface-3); border: 1px solid var(--soft-border); }
.panel-icon .icon, .panel-icon svg { width: 19px; height: 19px; }
.page-art { min-width: 118px; min-height: 86px; border: 1px solid var(--soft-border); border-radius: 8px; background: var(--surface-3); color: var(--accent-strong); display: grid; place-items: center; position: relative; }
.page-art > .icon, .page-art svg { width: 42px; height: 42px; }
.skills-art { display: grid; align-content: center; gap: 8px; padding: 14px; }
.art-node { display: block; min-width: 92px; padding: 7px 9px; border-radius: 6px; background: var(--surface); border: 1px solid var(--line); color: var(--text); font-size: 12px; font-weight: 800; box-shadow: 0 8px 18px var(--shadow-soft); }
.art-node:nth-child(2) { transform: translateX(18px); color: var(--accent-strong); }
.art-node:nth-child(3) { transform: translateX(4px); color: var(--clay); }
.cell-title { display: grid; gap: 3px; }
.table-action { display: inline-flex; align-items: center; gap: 6px; text-decoration: none; font-weight: 800; }
.inline-danger { margin: -8px 0 18px; }
.login-shell { min-height: calc(100vh - 126px); display: grid; grid-template-columns: minmax(0, 1fr) 420px; gap: 34px; align-items: center; max-width: 1040px; margin: 0 auto; }
.product-lockup { display: flex; gap: 14px; align-items: center; margin-bottom: 18px; }
.login-brand h1 { font-size: 48px; margin: 0; }
.login-brand p { color: var(--muted); font-size: 18px; max-width: 520px; }
.login-panel { padding: 26px; border-top: 3px solid var(--accent); }
.mark { width: 52px; height: 52px; border-radius: 10px; font-size: 18px; }
.login-graphic { margin-top: 28px; width: min(460px, 100%); display: grid; gap: 10px; }
.stack-card { display: flex; align-items: center; justify-content: space-between; min-height: 54px; padding: 12px 14px; border-radius: 8px; border: 1px solid var(--line); background: var(--surface); box-shadow: 0 10px 22px var(--shadow-soft); font-weight: 800; }
.stack-card span { color: var(--muted); }
.stack-card strong { color: var(--accent-strong); }
.stack-card.primary { border-left: 4px solid var(--accent); transform: translateX(18px); }
.stack-card.muted-card { border-left: 4px solid var(--clay); transform: translateX(34px); }
.diff { background: var(--surface); border: 1px solid var(--line); border-radius: 6px; padding: 0; }
.diff span { display: block; padding: 2px 8px; }
.diff .add { background: #e7f6e7; color: #14532d; }
.diff .del { background: #fde8e8; color: #842029; }
.diff .ctx { color: #4b5563; }
@media (max-width: 820px) {
	main { padding: 18px; }
	.split, .split.wide, .editor, .login-shell { grid-template-columns: 1fr; }
	.help-intent-grid, .help-cards { grid-template-columns: 1fr; }
	.form-grid { grid-template-columns: 1fr; gap: 0; }
	.inline-search { grid-template-columns: 1fr; }
	.pagehead { display: block; padding: 20px; }
	.page-art { display: none; }
	.sticky, .preview-pane { position: static; max-height: none; }
	nav { padding: 0 16px; gap: 10px; overflow-x: auto; align-items: center; }
	.nav-links { gap: 2px; }
	.brand span:last-child { display: none; }
	.nav-links a span:not(.icon), .theme-toggle .button-label, .nav-logout .button-label { display: none; }
	.surface { overflow: visible; }
	table, thead, tbody, tr, th, td { display: block; width: 100%; }
	thead { display: none; }
	tr { padding: 14px 16px; border-bottom: 1px solid var(--line); }
	tr:last-child { border-bottom: 0; }
	td { padding: 4px 0; border-bottom: 0; }
	tr:hover td { background: transparent; }
	td:empty { display: none; }
	td form { margin-top: 8px; }
	.token-cell code { max-width: 220px; }
	.detail-meta { grid-template-columns: 1fr; }
	h1 { font-size: 26px; }
	.login-brand h1 { font-size: 38px; }
	.stack-card.primary, .stack-card.muted-card { transform: none; }
}
`
