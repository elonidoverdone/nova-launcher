package dev.nova.fx;

import java.util.List;

import net.minecraft.client.DeltaTracker;
import net.minecraft.client.Minecraft;
import net.minecraft.client.gui.Font;
import net.minecraft.client.gui.GuiGraphicsExtractor;
import net.minecraft.network.chat.Component;

import net.fabricmc.fabric.api.client.rendering.v1.hud.HudElement;

/**
 * Shows only the line being sung right now, typed letter by letter (each letter fades in and
 * rises into place) on a rounded plate. Every new line picks a random spot on screen and the
 * plate glides there smoothly; the previous line fades out where it was.
 */
public final class LyricsHud implements HudElement {
	private static final float LETTER_FADE_MS = 160f;
	private static final float GLIDE_MS = 650f;
	private static final float OUT_MS = 260f;
	private static final int PAD_X = 6;
	private static final int PAD_Y = 4;
	private static final int RADIUS = 5;

	/** A line on screen (the current one, or the previous one fading out). */
	private static final class Shown {
		String text = "";
		int[] cps = new int[0]; // code points
		long lineStartPos;      // playback ms when the line starts
		long revealMs = 1;      // how long typing the whole line takes
		float x, y;             // top-left of the plate, in scaled units
		float fromX, fromY;
		long glideStart;        // wall clock
		long goneAt;            // wall clock when it started fading out (prev line only)
		long typeDelay;         // ms to wait before typing (lets the old line fade first)
		long seed;              // per-line randomness for letter placement
	}

	private Shown current;
	private Shown previous;
	private int currentIndex = Integer.MIN_VALUE;
	private String trackKey = "";
	private float plateW;       // animated plate width
	private long lastFrame;

	@Override
	public void extractRenderState(GuiGraphicsExtractor g, DeltaTracker deltaTracker) {
		FxConfig cfg = NovaFX.config();
		LyricsState.Data data = LyricsState.data();
		long now = System.currentTimeMillis();
		float dt = lastFrame == 0 ? 0 : Math.min(0.1f, (now - lastFrame) / 1000f);
		lastFrame = now;

		Font font = Minecraft.getInstance().font;
		float scale = cfg.lyricsScale;
		int screenW = g.guiWidth();
		int screenH = g.guiHeight();

		boolean active = cfg.lyricsEnabled && "ok".equals(data.status) && !data.lines.isEmpty();
		if (!active) {
			retire(now);
		} else {
			String key = data.artist + "\u0000" + data.title;
			if (!key.equals(trackKey)) {
				trackKey = key;
				currentIndex = Integer.MIN_VALUE;
				retire(now);
			}
			long pos = LyricsState.positionMs(cfg.lyricsOffsetMs);
			int i = LyricsState.activeIndex(pos);
			if (i != currentIndex) {
				currentIndex = i;
				startLine(data, i, font, scale, screenW, screenH, cfg, now);
			}
			if (current != null) {
				drawLine(g, font, cfg, current, pos, now, 1f, dt);
			}
		}
		if (previous != null) {
			float k = (now - previous.goneAt) / OUT_MS;
			if (k >= 1f) {
				previous = null;
			} else {
				drawLine(g, font, cfg, previous, Long.MAX_VALUE / 4, now, 1f - Draw.easeOut(k), 0f);
			}
		}
	}

	private void retire(long now) {
		if (current != null) {
			previous = current;
			previous.goneAt = now;
			current = null;
		}
	}

	private void startLine(LyricsState.Data data, int i, Font font, float scale, int screenW, int screenH, FxConfig cfg, long now) {
		List<LyricsState.Line> lines = data.lines;
		String text;
		long start;
		long end;
		if (i < 0) {
			// before the first line: show the song name
			text = "♪ " + (data.artist.isEmpty() ? "" : data.artist + " — ") + data.title;
			start = 0;
			end = lines.get(0).t;
		} else {
			LyricsState.Line l = lines.get(i);
			text = l.text == null || l.text.isBlank() ? "♪" : l.text;
			start = l.t;
			end = i + 1 < lines.size() ? lines.get(i + 1).t : l.t + 4000;
		}
		Shown s = new Shown();
		s.text = text;
		s.cps = text.codePoints().toArray();
		s.seed = text.hashCode() * 1_000_003L + i;
		s.lineStartPos = start;
		long duration = Math.max(400, end - start);
		long natural = (long) (s.cps.length * 55 / cfg.lyricsTypeSpeed);
		s.revealMs = Math.max(1, Math.min(natural, (long) (duration * 0.75)));

		int textW = font.width(Component.literal(text));
		float boxW = (textW + PAD_X * 2) * scale;
		float boxH = (font.lineHeight + PAD_Y * 2 + (cfg.lyrics3D ? cfg.lyricsScatter * 10 : 0)) * scale;
		float[] spot = cfg.lyricsRandomPos
				? randomSpot(data.title.hashCode() * 31L + i, boxW, boxH, screenW, screenH)
				: new float[] {screenW - boxW - 8, 58};

		Shown old = current;
		if (old != null) {
			old.goneAt = now;
			previous = old;
			s.fromX = old.x;
			s.fromY = old.y;
			s.typeDelay = Math.min(240, s.revealMs / 3 + 80);
		} else {
			s.fromX = spot[0];
			s.fromY = spot[1] + 8;
			plateW = 0;
		}
		s.x = spot[0];
		s.y = spot[1];
		s.glideStart = now;
		current = s;
	}

	/** Deterministic pseudo-random 0..1 for letter c of a line. */
	private static float rnd(long seed, int c, int k) {
		long z = seed * 0x9E3779B97F4A7C15L + c * 0xBF58476D1CE4E5B9L + k * 0x94D049BB133111EBL;
		z = (z ^ (z >>> 30)) * 0xBF58476D1CE4E5B9L;
		z = (z ^ (z >>> 27)) * 0x94D049BB133111EBL;
		z ^= z >>> 31;
		return (z >>> 40) / (float) (1L << 24);
	}

	/** A spot that keeps the plate on screen and away from the crosshair and hotbar. */
	static float[] randomSpot(long seed, float boxW, float boxH, int screenW, int screenH) {
		java.util.Random r = new java.util.Random(seed);
		float minX = 8, maxX = Math.max(minX, screenW - boxW - 8);
		float minY = 10, maxY = Math.max(minY, screenH - boxH - 48);
		float cx = screenW / 2f, cy = screenH / 2f;
		float[] best = {minX, minY};
		for (int attempt = 0; attempt < 12; attempt++) {
			float x = minX + r.nextFloat() * (maxX - minX);
			float y = minY + r.nextFloat() * (maxY - minY);
			best[0] = x;
			best[1] = y;
			boolean overCrosshair = x < cx + 60 && x + boxW > cx - 60 && y < cy + 30 && y + boxH > cy - 30;
			if (!overCrosshair) break;
		}
		return best;
	}

	private void drawLine(GuiGraphicsExtractor g, Font font, FxConfig cfg, Shown s, long pos, long now, float fade, float dt) {
		if (fade <= 0.01f) return;
		float scale = cfg.lyricsScale;
		float elapsed = pos - s.lineStartPos - s.typeDelay;

		// glide from the previous spot to this line's spot
		float k = Draw.easeInOut((now - s.glideStart) / GLIDE_MS);
		float x = s.fromX + (s.x - s.fromX) * k;
		float y = s.fromY + (s.y - s.fromY) * k;
		if (fade < 1f) {
			y -= (1f - fade) * 6f; // fading line drifts up a little
		}

		// how much of the text is typed so far, in letters (fractional)
		float typed = s.cps.length * Math.clamp(elapsed / (float) s.revealMs, 0f, 1f);
		int fullCount = s.cps.length;
		int visibleCount = Math.min(fullCount, (int) Math.ceil(typed));
		String visible = new String(s.cps, 0, visibleCount);
		int targetW = font.width(Component.literal(visible)) + PAD_X * 2;
		if (s == current) {
			plateW = dt == 0 ? targetW : Draw.approach(plateW, targetW, 18f, dt);
		}
		int w = s == current ? Math.round(plateW) : font.width(Component.literal(s.text)) + PAD_X * 2;
		float spread = cfg.lyrics3D ? cfg.lyricsScatter : 0f;
		int extraY = Math.round(spread * 5);
		int h = font.lineHeight + PAD_Y * 2 + extraY * 2;
		if (visibleCount == 0 && s == current) return;

		g.pose().pushMatrix();
		g.pose().translate(x, y);
		g.pose().scale(scale);

		if (cfg.lyricsBackground) {
			int bg = Draw.withAlpha(0x0E0C1E, Math.round(255 * cfg.lyricsBgOpacity * fade));
			Draw.roundRect(g, 0, 0, Math.max(h, w), h, RADIUS + extraY / 2, bg);
		}

		int color = FxConfig.argb(cfg.lyricsColor, 0xFFFFFFFF) & 0xFFFFFF;
		int depthColor = Draw.lerpColor(0xFF000000 | color, 0xFF000000, 0.65f) & 0xFFFFFF;
		float msPerLetter = s.revealMs / (float) Math.max(1, fullCount);
		float time = (now % 3_600_000L) / 1000f; // keep it small so float stays precise
		int baseY = PAD_Y + extraY;
		int cx = PAD_X;
		for (int c = 0; c < visibleCount; c++) {
			String ch = new String(s.cps, c, 1);
			int cw = font.width(Component.literal(ch));
			// each letter fades in and rises into place
			float letterT = (elapsed - c * msPerLetter) / LETTER_FADE_MS;
			float la = s == current ? Draw.easeOut(letterT) : 1f;
			float a = la * fade;
			if (a > 0.03f) {
				int alpha = Math.round(255 * a);
				if (!cfg.lyrics3D) {
					int dy = Math.round((1f - la) * 4f);
					g.text(font, Component.literal(ch), cx, baseY + dy, Draw.withAlpha(color, alpha), true);
				} else {
					// every letter has its own resting spot and tilt, and bobs gently around it
					float r1 = rnd(s.seed, c, 1), r2 = rnd(s.seed, c, 2), r3 = rnd(s.seed, c, 3);
					float ox = spread * (r1 - 0.5f) * 1.0f;
					float oy = spread * (r2 - 0.5f) * 7f + spread * (float) Math.sin(time * 2.1f + c * 0.65f) * 1.6f;
					float tilt = spread * ((r3 - 0.5f) * 0.38f + (float) Math.sin(time * 1.4f + c) * 0.07f);
					// letters swing in like turning cards
					float turn = Math.max(0.08f, la);
					float lift = (1f - la) * 6f;

					g.pose().pushMatrix();
					g.pose().translate(cx + cw / 2f + ox, baseY + font.lineHeight / 2f + oy + lift);
					g.pose().rotateAbout(tilt, 0f, 0f);
					g.pose().scale(turn, 1f);
					// extruded depth: a few darker copies behind the letter
					for (int d = 3; d >= 1; d--) {
						g.pose().pushMatrix();
						g.pose().translate(d * 0.55f, d * 0.55f);
						g.text(font, Component.literal(ch), -cw / 2, -font.lineHeight / 2, Draw.withAlpha(depthColor, Math.round(alpha * (1f - d * 0.18f))), false);
						g.pose().popMatrix();
					}
					g.text(font, Component.literal(ch), -cw / 2, -font.lineHeight / 2, Draw.withAlpha(color, alpha), false);
					g.pose().popMatrix();
				}
			}
			cx += cw;
		}
		g.pose().popMatrix();
	}
}
