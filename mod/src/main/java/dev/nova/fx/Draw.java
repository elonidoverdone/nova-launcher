package dev.nova.fx;

import net.minecraft.client.gui.GuiGraphicsExtractor;

/** Small drawing helpers: rounded rectangles, colour math, easing. */
public final class Draw {
	private Draw() {
	}

	/** Filled rectangle with rounded corners, drawn as horizontal strips so nothing overlaps. */
	public static void roundRect(GuiGraphicsExtractor g, int x1, int y1, int x2, int y2, int radius, int argb) {
		if (x2 <= x1 || y2 <= y1 || (argb >>> 24) == 0) return;
		int r = Math.max(0, Math.min(radius, Math.min((x2 - x1) / 2, (y2 - y1) / 2)));
		for (int i = 0; i < r; i++) {
			double dy = r - i - 0.5;
			int inset = (int) Math.round(r - Math.sqrt(Math.max(0, r * r - dy * dy)));
			g.fill(x1 + inset, y1 + i, x2 - inset, y1 + i + 1, argb);
			g.fill(x1 + inset, y2 - i - 1, x2 - inset, y2 - i, argb);
		}
		g.fill(x1, y1 + r, x2, y2 - r, argb);
	}

	/** Rounded outline, 1px. */
	public static void roundOutline(GuiGraphicsExtractor g, int x1, int y1, int x2, int y2, int radius, int argb) {
		int r = Math.max(0, Math.min(radius, Math.min((x2 - x1) / 2, (y2 - y1) / 2)));
		g.fill(x1 + r, y1, x2 - r, y1 + 1, argb);
		g.fill(x1 + r, y2 - 1, x2 - r, y2, argb);
		g.fill(x1, y1 + r, x1 + 1, y2 - r, argb);
		g.fill(x2 - 1, y1 + r, x2, y2 - r, argb);
		// corner arcs: each row spans from its own inset to the previous row's inset
		int prev = r;
		for (int i = 0; i < r; i++) {
			double dy = r - i - 0.5;
			int inset = (int) Math.round(r - Math.sqrt(Math.max(0, r * r - dy * dy)));
			int span = Math.max(1, prev - inset);
			g.fill(x1 + inset, y1 + i, x1 + inset + span, y1 + i + 1, argb);
			g.fill(x2 - inset - span, y1 + i, x2 - inset, y1 + i + 1, argb);
			g.fill(x1 + inset, y2 - i - 1, x1 + inset + span, y2 - i, argb);
			g.fill(x2 - inset - span, y2 - i - 1, x2 - inset, y2 - i, argb);
			prev = inset;
		}
	}

	/** Multiplies the alpha of an ARGB colour by a (0..1). */
	public static int alpha(int argb, float a) {
		int base = argb >>> 24;
		int out = Math.round(base * Math.clamp(a, 0f, 1f));
		return (out << 24) | (argb & 0x00FFFFFF);
	}

	public static int withAlpha(int rgb, int alpha255) {
		return (Math.clamp(alpha255, 0, 255) << 24) | (rgb & 0x00FFFFFF);
	}

	public static int lerpColor(int a, int b, float t) {
		t = Math.clamp(t, 0f, 1f);
		int aa = a >>> 24, ar = (a >> 16) & 0xFF, ag = (a >> 8) & 0xFF, ab = a & 0xFF;
		int ba = b >>> 24, br = (b >> 16) & 0xFF, bg = (b >> 8) & 0xFF, bb = b & 0xFF;
		return (Math.round(aa + (ba - aa) * t) << 24) | (Math.round(ar + (br - ar) * t) << 16)
				| (Math.round(ag + (bg - ag) * t) << 8) | Math.round(ab + (bb - ab) * t);
	}

	public static float easeOut(float t) {
		t = Math.clamp(t, 0f, 1f);
		float u = 1f - t;
		return 1f - u * u * u;
	}

	public static float easeInOut(float t) {
		t = Math.clamp(t, 0f, 1f);
		return t < 0.5f ? 4 * t * t * t : 1 - (float) Math.pow(-2 * t + 2, 3) / 2;
	}

	/** Frame-rate independent approach of current towards target. */
	public static float approach(float current, float target, float speedPerSecond, float dtSeconds) {
		float k = 1f - (float) Math.exp(-speedPerSecond * dtSeconds);
		return current + (target - current) * k;
	}
}
