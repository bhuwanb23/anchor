---
format: 1920x1080
duration: 60s
message: "Own your server. Don't babysit it."
arc: Before → After tease → Bridge (product) → Steps 1-2-3 → CTA
audience: "freelance developers and small agencies babysitting a $5–40 VPS with no sysadmin"
music: "calm, warm, confident, minimal electronic underscore, gentle pulse ~92 BPM, no vocals"
captions: enabled
---

## Video direction

- **Palette system (from frame.md):** warm cream ground `#F6F8FA` everywhere; single cobalt `#1F6FEB` carries every accent (eyebrow, numeral, CTA, progress bar, highlight rule); headlines near-black `#0B1B2B` (never cobalt); body muted `#0055D6`. Tinted cards: 4% cobalt fill / 20% cobalt 1.5px border / 10–14px radius, **no shadows**.
- **Motion defaults + shot model:** every frame is a directed shot — `entrance → development → settle`. Default ease `power3.out` for arrivals, `power2.inOut` for repositioning, `circ.inOut` for camera moves. Idle budget: ONE living element per frame maximum (wordmark glow, camera micro-drift, or a CTA sheen) — everything else settles and holds.
- **Negative list:** no slideshow enter-then-freeze; no screensaver (everything drifting independently); no purple-blue AI gradients; no invented product screenshots, domain names, or live URLs (project is pre-launch — use "your domain" wording and the repo's own diagrams only); no drop shadows on content; no second accent color.
- **Stillness allocation:** Frame 6 (CTA) is the sole held frame — wordmark + ring pulse only, letting the promise land. Every other frame develops mid-shot.
- **Caption band:** bottom ~180px is the caption pill's zone; all primary content lives above it.

---

## Frame 1 — Hook: Your server, your business

- scene: Cover treatment — cream ground, diagonal cobalt-tint panel at right, the Anchor wordmark (hero-banner) as the dominant block under a cobalt accent-line
- voiceover: "You own a server. It runs your business. It shouldn't run you."
- duration: 7s
- transition_in: cut
- status: outline
- src: compositions/frames/01-hook.html
- type: hook
- persuasion: Pain validation
- beat: tension
- asset_candidates: assets/hero-banner.png — Anchor wordmark + subline on soft gradient, wide banner
- effects: spring-pop-entrance, sine-wave-loop, ambient-glow-bloom
- focal: assets/hero-banner.png
- roles: hero-banner = foreground hero (dominant, ~55% width, anchored left-center); diagonal panel = background accent

narrativeRole: Name the promise cold — you own the box, it shouldn't own you. Direct-address hook for the exact audience.
keyMessage: Your server should serve you, not the reverse.

## Frame 2 — The babysit loop

- scene: Split pain grid — cream ground, three tinted cards (full disk / expired TLS / 2AM alert) that rise and stack while a calm baseline holds
- voiceover: "Tonight it's a full disk. Tomorrow, an expired certificate. Another hour gone to a machine you already pay for."
- duration: 9s
- transition_in: crossfade
- status: outline
- src: compositions/frames/02-pain.html
- type: pain_point
- persuasion: Cognitive overload
- beat: anxiety → frustration
- asset_candidates: none — typography + frame.md tinted-card primitives only
- effects: waterfall-entry, ambient-glow-bloom, vignette hold
- focal: the three stacked pain cards
- roles: pain cards = foreground sequence; background = cream + faint dot grid only

narrativeRole: Agitate the known pain with concrete nightly failures — the viewer should nod.
keyMessage: Manual server care is a slow, invisible tax.

## Frame 3 — After tease: One dashboard

- scene: Cream ground open sweep — headline "Your server, managed." with the promise line settling below; empty space becomes the product
- voiceover: "Anchor turns it into your own private cloud — and the dashboard is the only thing you touch."
- duration: 8s
- transition_in: push-slide RIGHT
- status: outline
- src: compositions/frames/03-tease.html
- type: product_intro
- persuasion: Simplification
- beat: relief → curiosity
- asset_candidates: none — pure type frame (cover scale, centered pulse)
- effects: waterfall-entry, gradient-text-sweep, sine-wave-loop
- focal: the "Your server, managed." headline (~60% width, centered above caption band)
- roles: headline = hero; supporting promise line = secondary beneath

narrativeRole: The BAB "after" — relief moment. Product named, promise spoken plainly.
keyMessage: One dashboard. That's the whole interface.

## Frame 4 — How it works (the mechanism)

- scene: architecture diagram (three columns: Your Server ↔ Control Plane ↔ Dashboard) revealed with a slow lateral camera pan left→right as the narration walks the flow
- voiceover: "A small agent lives on your server. Your apps stay on hardware you own with automatic HTTPS — while health and commands travel over one connection."
- duration: 12s
- transition_in: push-slide RIGHT
- status: outline
- src: compositions/frames/04-mechanism.html
- type: feature_showcase
- persuasion: Show-don't-tell proof
- beat: clarity
- asset_candidates: assets/architecture.png — labeled three-column system diagram (agent, control plane, dashboard)
- effects: viewport-change, ambient-glow-bloom, coordinate-target-zoom
- focal: assets/architecture.png (fills ~70% width, top-anchored above caption band)
- roles: architecture diagram = full-frame hero background-document; headline chip = foreground label

narrativeRole: Prove it with the real architecture — three boxes, two connections, nothing to maintain.
keyMessage: Agent on your hardware, control plane only orchestrates, dashboard is all you use.

## Frame 5 — What the agent does for you

- scene: agent-layers diagram seat with three value chips (Auto HTTPS · Nightly backups · Plain-English alerts) rising in sequence, each landing with a soft emphasis
- voiceover: "It deploys from Git or Docker, backs everything up every night, and if something breaks, it tells you in plain English what happened."
- duration: 12s
- transition_in: push-slide RIGHT
- status: outline
- src: compositions/frames/05-value.html
- type: benefit_highlight
- persuasion: Feature-to-benefit translation (rule of three)
- beat: confidence → control
- asset_candidates: assets/agent-layers.png — agent mental-model diagram (4A/4B/4C over 3A/3B/3C)
- effects: waterfall-entry, spring-pop-entrance, ambient-glow-bloom
- focal: assets/agent-layers.png (background, dimmed ~35%) with the three chips as foreground sequence
- roles: agent-layers = soft backdrop proof; three benefit chips = foreground, top-center, above caption band

narrativeRole: Convert the mechanism into three felt benefits — the viewer remembers 3 concrete wins.
keyMessage: Deploys, backups, alerts — handled, in plain English.

## Frame 6 — CTA: Bring your own server

- scene: Closing treatment — concentric cobalt rings behind, Anchor wordmark centered, one solid cobalt CTA pill ("Bring your own server — placeholder domain"), progress bar completed
- voiceover: "Bring any five-dollar box. Leave the babysitting to Anchor."
- duration: 10s
- transition_in: cut
- status: outline
- src: compositions/frames/06-cta.html
- type: cta
- persuasion: Empowerment and control
- beat: peace of mind
- asset_candidates: assets/hero-banner.png — Anchor wordmark banner
- effects: ambient-glow-bloom, spring-pop-entrance, svg-icon-enrichment
- focal: the wordmark + CTA pill centered above caption band
- roles: hero-banner wordmark = hero; concentric rings = background atmosphere; CTA pill = single solid cobalt element

narrativeRole: Held, quiet close — the promise lands and the CTA is the single solid element on screen.
keyMessage: It's yours. Anchor just keeps it running.

---

## Pacing notes

- Est. total 58s before audio resync (target ~60s).
- Frames 4–5 form one left-to-right "how it works / what you get" flow (same push-slide RIGHT transition).
- Real Kokoro voice durations win in Step 5 sync; estimates above are guides.
