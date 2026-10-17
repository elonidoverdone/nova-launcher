package dev.nova.fx;

import java.util.Locale;

import net.minecraft.client.DeltaTracker;
import net.minecraft.client.Minecraft;
import net.minecraft.client.gui.GuiGraphicsExtractor;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.player.Player;
import net.minecraft.world.phys.EntityHitResult;
import net.minecraft.world.phys.HitResult;

import net.fabricmc.fabric.api.client.rendering.v1.hud.HudElement;

/**
 * A spinning star over the player or mob under the crosshair, shown only when it is within
 * attack reach (vanilla's crosshair target is only set within interaction range). It pops in,
 * spins, sizes itself to the target's distance and pulses on every hit.
 */
public final class TargetMarkerHud implements HudElement {
	private float shown;      // 0..1 appear/disappear
	private float sizeAnim;   // smoothed on-screen radius
	private float pulse;      // 1 right after a hit
	private long lastFrame;

	public void hit() {
		pulse = 1f;
	}

	/** The entity we would hit right now, or null. */
	static Entity target(Minecraft mc, FxConfig cfg) {
		HitResult hr = mc.hitResult;
		if (mc.player == null || !(hr instanceof EntityHitResult ehr)) return null;
		Entity e = ehr.getEntity();
		if (!(e instanceof LivingEntity living) || !living.isAlive() || e == mc.player) return null;
		if (cfg.targetOnlyPlayers && !(e instanceof Player)) return null;
		return e;
	}

	@Override
	public void extractRenderState(GuiGraphicsExtractor g, DeltaTracker deltaTracker) {
		FxConfig cfg = NovaFX.config();
		Minecraft mc = Minecraft.getInstance();
		long now = System.currentTimeMillis();
		float dt = lastFrame == 0 ? 0 : Math.min(0.1f, (now - lastFrame) / 1000f);
		lastFrame = now;

		Entity t = cfg.targetEnabled ? target(mc, cfg) : null;
		shown = Draw.approach(shown, t != null ? 1f : 0f, t != null ? 12f : 8f, dt);
		pulse = Draw.approach(pulse, 0f, 7f, dt);
		if (shown < 0.02f) return;

		if (t != null) {
			// on-screen size of the target's body: height / (2 * tan(fov/2) * distance) * screen height
			double d = Math.sqrt(mc.player.distanceToSqr(t.getX(), t.getY() + t.getBbHeight() * 0.5, t.getZ()));
			float body = (float) (t.getBbHeight() / (1.4 * Math.max(0.5, d)) * g.guiHeight());
			float target = Math.clamp(body * 0.42f * cfg.targetSize, 8f, 140f);
			sizeAnim = sizeAnim == 0 ? target : Draw.approach(sizeAnim, target, 10f, dt);
		}

		float appear = Draw.easeOut(shown);
		float radius = sizeAnim * (0.55f + 0.45f * appear) * (1f + 0.18f * pulse);
		// spins flat in the screen plane; direction is set in the menu
		float dir = "LEFT".equalsIgnoreCase(cfg.targetDirection) ? -1f : 1f;
		// angle in double, wrapped to one turn: float can't hold epoch milliseconds precisely
		double turns = (now % 3_600_000L) / 1000.0 * cfg.targetSpeed;
		float spin = dir * (float) ((turns - Math.floor(turns)) * Math.PI * 2);
		int color = FxConfig.argb(cfg.targetColor, 0xFF3D7BFF);
		int alpha = Math.round(235 * appear);

		g.pose().pushMatrix();
		g.pose().translate(g.guiWidth() / 2f, g.guiHeight() / 2f);
		g.pose().rotateAbout(spin, 0f, 0f);

		float[][] pts = shape(cfg.targetShape, radius);
		if (cfg.targetGlow) {
			int glow = Draw.withAlpha(color, Math.round(alpha * 0.28f));
			for (float[] seg : pts) line(g, seg, 3, glow);
		}
		int core = Draw.withAlpha(Draw.lerpColor(color, 0xFFFFFFFF, 0.25f * pulse), alpha);
		for (float[] seg : pts) line(g, seg, 1, core);
		g.pose().popMatrix();
	}

	/** Line segments {x1,y1,x2,y2} of the chosen shape, centred on 0,0. */
	static float[][] shape(String name, float r) {
		switch (name == null ? "" : name.toUpperCase(Locale.ROOT)) {
			case "STAR": {
				// pentagram: connect every second vertex of a pentagon
				float[][] v = polygon(5, r, -90);
				float[][] out = new float[5][];
				for (int i = 0; i < 5; i++) {
					float[] a = v[i], b = v[(i + 2) % 5];
					out[i] = new float[] {a[0], a[1], b[0], b[1]};
				}
				return out;
			}
			case "DIAMOND":
				return edges(polygon(4, r, -90));
			case "RING":
				return edges(polygon(28, r, 0));
			default: {
				// hexagram: two overlapping triangles
				float[][] up = edges(polygon(3, r, -90));
				float[][] down = edges(polygon(3, r, 90));
				float[][] out = new float[6][];
				System.arraycopy(up, 0, out, 0, 3);
				System.arraycopy(down, 0, out, 3, 3);
				return out;
			}
		}
	}

	private static float[][] polygon(int n, float r, float startDeg) {
		float[][] v = new float[n][];
		for (int i = 0; i < n; i++) {
			double a = Math.toRadians(startDeg + 360.0 * i / n);
			v[i] = new float[] {(float) (Math.cos(a) * r), (float) (Math.sin(a) * r)};
		}
		return v;
	}

	private static float[][] edges(float[][] v) {
		float[][] out = new float[v.length][];
		for (int i = 0; i < v.length; i++) {
			float[] a = v[i], b = v[(i + 1) % v.length];
			out[i] = new float[] {a[0], a[1], b[0], b[1]};
		}
		return out;
	}

	/** A straight segment drawn as a rotated thin rectangle. */
	private static void line(GuiGraphicsExtractor g, float[] s, int halfThick, int argb) {
		float dx = s[2] - s[0], dy = s[3] - s[1];
		float len = (float) Math.sqrt(dx * dx + dy * dy);
		if (len < 0.5f) return;
		g.pose().pushMatrix();
		g.pose().translate(s[0], s[1]);
		g.pose().rotateAbout((float) Math.atan2(dy, dx), 0f, 0f);
		// extend a little so corners close neatly
		g.fill(-halfThick, -halfThick, Math.round(len) + halfThick, halfThick, argb);
		g.pose().popMatrix();
	}
}
