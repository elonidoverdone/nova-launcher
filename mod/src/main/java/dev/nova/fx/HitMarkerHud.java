package dev.nova.fx;

import net.minecraft.client.DeltaTracker;
import net.minecraft.client.gui.GuiGraphicsExtractor;

import net.fabricmc.fabric.api.client.rendering.v1.hud.HudElement;

/** Small "X" around the crosshair after a hit; red after a kill. */
public final class HitMarkerHud implements HudElement {
	private static final int DURATION_TICKS = 6;

	private int ticksLeft;
	private boolean kill;

	public void hit() {
		ticksLeft = DURATION_TICKS;
		kill = false;
	}

	public void kill() {
		ticksLeft = DURATION_TICKS + 4;
		kill = true;
	}

	public void tick() {
		if (ticksLeft > 0) ticksLeft--;
	}

	@Override
	public void extractRenderState(GuiGraphicsExtractor graphics, DeltaTracker deltaTracker) {
		FxConfig cfg = NovaFX.config();
		if (ticksLeft <= 0 || !cfg.enabled || !cfg.hitMarker) return;

		int color = kill
				? FxConfig.argb(cfg.killMarkerColor, 0xFFFF4D5E)
				: FxConfig.argb(cfg.hitMarkerColor, 0xFFFFFFFF);
		int alpha = Math.min(255, 70 + ticksLeft * 40);
		color = (alpha << 24) | (color & 0x00FFFFFF);

		int cx = graphics.guiWidth() / 2;
		int cy = graphics.guiHeight() / 2;
		int gap = 3;
		int len = kill ? 5 : 4;
		// four diagonal strokes drawn as 1px squares
		for (int i = 0; i < len; i++) {
			int d = gap + i;
			dot(graphics, cx - d, cy - d, color);
			dot(graphics, cx + d, cy - d, color);
			dot(graphics, cx - d, cy + d, color);
			dot(graphics, cx + d, cy + d, color);
		}
	}

	private static void dot(GuiGraphicsExtractor g, int x, int y, int color) {
		g.fill(x, y, x + 1, y + 1, color);
	}
}
