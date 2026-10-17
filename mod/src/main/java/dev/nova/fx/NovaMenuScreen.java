package dev.nova.fx;

import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.function.Consumer;
import java.util.function.DoubleFunction;
import java.util.function.Supplier;

import net.minecraft.client.gui.GuiGraphicsExtractor;
import net.minecraft.client.gui.screens.Screen;
import net.minecraft.client.input.KeyEvent;
import net.minecraft.client.input.MouseButtonEvent;
import net.minecraft.network.chat.Component;

/**
 * NovaFX settings, opened with Right Shift (in a world, on the title screen and in the pause menu).
 * Everything is drawn by hand: rounded panels, animated switches, sliders, choice pickers, colour swatches.
 */
public final class NovaMenuScreen extends Screen {
	// palette
	private static final int PANEL = 0xF2151229;
	private static final int SIDEBAR = 0xFF1B1735;
	private static final int ROW_HOVER = 0x14FFFFFF;
	private static final int LINE = 0xFF352F61;
	private static final int INK = 0xFFECE9FF;
	private static final int MUTED = 0xFF9A94C4;
	private static final int TRACK = 0xFF2E2856;
	private static final int[] SWATCHES = {
			0xFFFFFFFF, 0xFFFFC857, 0xFFFF4D5E, 0xFFFF7AD9, 0xFF9B7BFF, 0xFF5CC8FF, 0xFF6FE0A8, 0xFF1A1A1A
	};

	private static final int ROW_H = 24;
	private static final long OPEN_MS = 220;

	private final Screen parent; // may be null (opened in-game)
	private final FxConfig cfg = NovaFX.config();
	private final List<Tab> tabs = new ArrayList<>();
	private int tab;
	private float scroll;
	private float scrollTarget;
	private Slider dragging;
	private final long openedAt = System.currentTimeMillis();
	private long lastFrame;
	private final float[] tabAnim = new float[8];

	// layout, recomputed each frame
	private int px, py, pw, ph, contentX, contentY, contentW, contentH;

	public NovaMenuScreen(Screen parent) {
		super(Component.literal("NovaFX"));
		this.parent = parent;
		buildTabs();
	}

	// ------------------------------------------------------------------ options

	private abstract static class Opt {
		final String label;
		float hover;

		Opt(String label) {
			this.label = label;
		}

		/** Called with mouse coordinates relative to the row's control area. */
		abstract void click(double mx, double rowX, double rowW);

		abstract void draw(NovaMenuScreen s, GuiGraphicsExtractor g, int x, int y, int w, int accent, float dt);
	}

	private final class Toggle extends Opt {
		final Supplier<Boolean> get;
		final Consumer<Boolean> set;
		float knob = -1;

		Toggle(String label, Supplier<Boolean> get, Consumer<Boolean> set) {
			super(label);
			this.get = get;
			this.set = set;
		}

		@Override
		void click(double mx, double rowX, double rowW) {
			set.accept(!get.get());
			changed();
		}

		@Override
		void draw(NovaMenuScreen s, GuiGraphicsExtractor g, int x, int y, int w, int accent, float dt) {
			boolean on = get.get();
			if (knob < 0) knob = on ? 1 : 0;
			knob = Draw.approach(knob, on ? 1 : 0, 16f, dt);
			int sw = 22, sh = 12;
			int sx = x + w - sw, sy = y + (ROW_H - sh) / 2;
			Draw.roundRect(g, sx, sy, sx + sw, sy + sh, sh / 2, Draw.lerpColor(TRACK, accent, knob));
			int kx = sx + 2 + Math.round(knob * (sw - sh));
			Draw.roundRect(g, kx, sy + 2, kx + sh - 4, sy + sh - 2, 4, INK);
		}
	}

	private final class Slider extends Opt {
		final double min, max, step;
		final Supplier<Double> get;
		final Consumer<Double> set;
		final DoubleFunction<String> fmt;
		int trackX, trackW;

		Slider(String label, double min, double max, double step, Supplier<Double> get, Consumer<Double> set, DoubleFunction<String> fmt) {
			super(label);
			this.min = min;
			this.max = max;
			this.step = step;
			this.get = get;
			this.set = set;
			this.fmt = fmt;
		}

		void setFromMouse(double mx) {
			double t = Math.clamp((mx - trackX) / Math.max(1, trackW), 0, 1);
			double v = min + t * (max - min);
			v = Math.round(v / step) * step;
			set.accept(Math.clamp(v, min, max));
		}

		@Override
		void click(double mx, double rowX, double rowW) {
			dragging = this;
			setFromMouse(mx);
		}

		@Override
		void draw(NovaMenuScreen s, GuiGraphicsExtractor g, int x, int y, int w, int accent, float dt) {
			String value = fmt.apply(get.get());
			int vw = s.font.width(Component.literal(value));
			g.text(s.font, value, x + w - vw, y + 8, MUTED, false);
			// fixed value column so all tracks line up
			int valueCol = 40;
			trackW = Math.min(110, w - valueCol - 10 - s.font.width(Component.literal(label)) - 12);
			trackW = Math.max(40, trackW);
			trackX = x + w - valueCol - 6 - trackW;
			int ty = y + ROW_H / 2 - 2;
			double t = (get.get() - min) / (max - min);
			Draw.roundRect(g, trackX, ty, trackX + trackW, ty + 4, 2, TRACK);
			int fillTo = trackX + (int) Math.round(t * trackW);
			Draw.roundRect(g, trackX, ty, Math.max(trackX + 4, fillTo), ty + 4, 2, accent);
			Draw.roundRect(g, fillTo - 4, ty - 3, fillTo + 4, ty + 7, 4, INK);
		}
	}

	private final class Choice extends Opt {
		final String[][] values; // {id, name}
		final Supplier<String> get;
		final Consumer<String> set;
		float nudge;

		Choice(String label, String[][] values, Supplier<String> get, Consumer<String> set) {
			super(label);
			this.values = values;
			this.get = get;
			this.set = set;
		}

		int index() {
			String cur = get.get();
			for (int i = 0; i < values.length; i++) {
				if (values[i][0].equalsIgnoreCase(cur)) return i;
			}
			return 0;
		}

		@Override
		void click(double mx, double rowX, double rowW) {
			// left part of the picker goes back, the rest goes forward
			boolean back = mx < rowX + rowW - 60;
			int i = index();
			i = back ? (i - 1 + values.length) % values.length : (i + 1) % values.length;
			set.accept(values[i][0]);
			nudge = back ? -1 : 1;
			changed();
		}

		@Override
		void draw(NovaMenuScreen s, GuiGraphicsExtractor g, int x, int y, int w, int accent, float dt) {
			nudge = Draw.approach(nudge, 0, 14f, dt);
			String name = values[index()][1];
			int nw = s.font.width(Component.literal(name));
			int boxW = Math.max(90, nw + 26);
			int bx = x + w - boxW, by = y + 4;
			Draw.roundRect(g, bx, by, bx + boxW, by + ROW_H - 8, 6, TRACK);
			g.text(s.font, "‹", bx + 5, by + 4, MUTED, false);
			g.text(s.font, "›", bx + boxW - 9, by + 4, MUTED, false);
			int tx = bx + (boxW - nw) / 2 + Math.round(nudge * 4);
			g.text(s.font, name, tx, by + 4, INK, false);
		}
	}

	private final class Colors extends Opt {
		final Supplier<String> get;
		final Consumer<String> set;

		Colors(String label, Supplier<String> get, Consumer<String> set) {
			super(label);
			this.get = get;
			this.set = set;
		}

		@Override
		void click(double mx, double rowX, double rowW) {
			int size = 12, gap = 4;
			int start = (int) (rowX + rowW) - SWATCHES.length * (size + gap) + gap;
			int i = (int) ((mx - start) / (size + gap));
			if (i >= 0 && i < SWATCHES.length) {
				set.accept(String.format(Locale.ROOT, "#%06X", SWATCHES[i] & 0xFFFFFF));
				changed();
			}
		}

		@Override
		void draw(NovaMenuScreen s, GuiGraphicsExtractor g, int x, int y, int w, int accent, float dt) {
			int size = 12, gap = 4;
			int cur = FxConfig.argb(get.get(), 0xFFFFFFFF);
			int sx = x + w - SWATCHES.length * (size + gap) + gap;
			int sy = y + (ROW_H - size) / 2;
			for (int i = 0; i < SWATCHES.length; i++) {
				int cx = sx + i * (size + gap);
				if ((SWATCHES[i] & 0xFFFFFF) == (cur & 0xFFFFFF)) {
					Draw.roundOutline(g, cx - 2, sy - 2, cx + size + 2, sy + size + 2, 5, INK);
				}
				Draw.roundRect(g, cx, sy, cx + size, sy + size, 4, SWATCHES[i]);
			}
		}
	}

	private record Tab(String name, List<Opt> opts) {
	}

	private void changed() {
		cfg.save();
	}

	private static String pct(double v) {
		return Math.round(v * 100) + "%";
	}

	private static String num(double v) {
		return String.format(Locale.ROOT, "%.1f", v);
	}

	private void buildTabs() {
		String[][] particles = {
				{"minecraft:crit", "Криты"}, {"minecraft:enchanted_hit", "Магия"}, {"minecraft:heart", "Сердца"},
				{"minecraft:flame", "Огонь"}, {"minecraft:soul_fire_flame", "Синий огонь"}, {"minecraft:electric_spark", "Искры"},
				{"minecraft:end_rod", "Звёзды"}, {"minecraft:totem_of_undying", "Тотем"}, {"minecraft:snowflake", "Снег"},
				{"minecraft:firework", "Фейерверк"}
		};
		String[][] shapes = {{"BURST", "Взрыв"}, {"RING", "Кольцо"}, {"SPIRAL", "Спираль"}};
		String[][] sounds = {
				{"none", "Без звука"}, {"minecraft:block.amethyst_block.chime", "Аметист"},
				{"minecraft:entity.experience_orb.pickup", "Опыт"}, {"minecraft:entity.arrow.hit_player", "Стрела"},
				{"minecraft:entity.player.attack.crit", "Крит"}, {"minecraft:block.note_block.pling", "Нотный блок"},
				{"minecraft:block.note_block.bell", "Колокольчик"}
		};
		String[][] kills = {{"NONE", "Нет"}, {"PILLAR", "Столб света"}, {"SOULS", "Души"}, {"EXPLOSION", "Взрыв"}, {"TOTEM", "Тотем"}};

		tabs.add(new Tab("Удары", List.of(
				new Toggle("Эффекты ударов", () -> cfg.enabled, v -> cfg.enabled = v),
				new Toggle("Только по игрокам", () -> cfg.onlyPlayers, v -> cfg.onlyPlayers = v),
				new Choice("Частицы", particles, () -> cfg.hitParticle, v -> cfg.hitParticle = v),
				new Choice("Форма", shapes, () -> cfg.hitShape, v -> cfg.hitShape = v),
				new Slider("Количество", 1, 80, 1, () -> (double) cfg.particleCount, v -> cfg.particleCount = (int) Math.round(v), v -> String.valueOf((int) Math.round(v))),
				new Slider("Скорость", 0.1, 3, 0.1, () -> (double) cfg.particleSpeed, v -> cfg.particleSpeed = v.floatValue(), NovaMenuScreen::num),
				new Choice("Звук удара", sounds, () -> cfg.hitSound, v -> cfg.hitSound = v),
				new Slider("Громкость", 0, 2, 0.05, () -> (double) cfg.soundVolume, v -> cfg.soundVolume = v.floatValue(), NovaMenuScreen::pct),
				new Slider("Тон", 0.5, 2, 0.05, () -> (double) cfg.soundPitch, v -> cfg.soundPitch = v.floatValue(), NovaMenuScreen::num)
		)));
		tabs.add(new Tab("Добивание", List.of(
				new Choice("Эффект", kills, () -> cfg.killEffect, v -> cfg.killEffect = v),
				new Choice("Звук", sounds, () -> cfg.killSound, v -> cfg.killSound = v)
		)));
		tabs.add(new Tab("Хитмаркер", List.of(
				new Toggle("Хитмаркер", () -> cfg.hitMarker, v -> cfg.hitMarker = v),
				new Colors("Попадание", () -> cfg.hitMarkerColor, v -> cfg.hitMarkerColor = v),
				new Colors("Добивание", () -> cfg.killMarkerColor, v -> cfg.killMarkerColor = v)
		)));
		String[][] targetShapes = {{"HEXAGRAM", "Гексаграмма"}, {"STAR", "Пятиконечная"}, {"DIAMOND", "Ромб"}, {"RING", "Кольцо"}};
		tabs.add(new Tab("Цель", List.of(
				new Toggle("Метка цели", () -> cfg.targetEnabled, v -> cfg.targetEnabled = v),
				new Toggle("Только игроки", () -> cfg.targetOnlyPlayers, v -> cfg.targetOnlyPlayers = v),
				new Choice("Форма", targetShapes, () -> cfg.targetShape, v -> cfg.targetShape = v),
				new Colors("Цвет", () -> cfg.targetColor, v -> cfg.targetColor = v),
				new Slider("Размер", 0.3, 3, 0.1, () -> (double) cfg.targetSize, v -> cfg.targetSize = v.floatValue(), v -> num(v) + "\u00D7"),
				new Slider("Скорость", 0, 3, 0.1, () -> (double) cfg.targetSpeed, v -> cfg.targetSpeed = v.floatValue(), v -> num(v) + " об/с"),
				new Choice("Вращение", new String[][] {{"RIGHT", "По часовой"}, {"LEFT", "Против часовой"}},
						() -> cfg.targetDirection, v -> cfg.targetDirection = v),
				new Toggle("Свечение", () -> cfg.targetGlow, v -> cfg.targetGlow = v)
		)));
		tabs.add(new Tab("Музыка", List.of(
				new Toggle("Текст песни", () -> cfg.lyricsEnabled, v -> cfg.lyricsEnabled = v),
				new Toggle("Случайное место", () -> cfg.lyricsRandomPos, v -> cfg.lyricsRandomPos = v),
				new Toggle("Объёмные буквы", () -> cfg.lyrics3D, v -> cfg.lyrics3D = v),
				new Slider("Разброс букв", 0, 1, 0.05, () -> (double) cfg.lyricsScatter, v -> cfg.lyricsScatter = v.floatValue(), NovaMenuScreen::pct),
				new Slider("Размер", 0.6, 3, 0.1, () -> (double) cfg.lyricsScale, v -> cfg.lyricsScale = v.floatValue(), v -> num(v) + "×"),
				new Slider("Скорость букв", 0.5, 3, 0.1, () -> (double) cfg.lyricsTypeSpeed, v -> cfg.lyricsTypeSpeed = v.floatValue(), v -> num(v) + "×"),
				new Colors("Цвет текста", () -> cfg.lyricsColor, v -> cfg.lyricsColor = v),
				new Toggle("Подложка", () -> cfg.lyricsBackground, v -> cfg.lyricsBackground = v),
				new Slider("Прозрачность", 0, 1, 0.05, () -> (double) cfg.lyricsBgOpacity, v -> cfg.lyricsBgOpacity = v.floatValue(), NovaMenuScreen::pct),
				new Slider("Сдвиг", -3000, 3000, 100, () -> (double) cfg.lyricsOffsetMs, v -> cfg.lyricsOffsetMs = (int) Math.round(v),
						v -> String.format(Locale.ROOT, "%+.1f с", v / 1000))
		)));
		tabs.add(new Tab("Меню", List.of(
				new Colors("Акцент", () -> cfg.menuAccent, v -> cfg.menuAccent = v)
		)));
	}

	// ------------------------------------------------------------------ rendering

	private void layout() {
		pw = Math.min(400, this.width - 20);
		ph = Math.min(250, this.height - 20);
		px = (this.width - pw) / 2;
		py = (this.height - ph) / 2;
		contentX = px + 112;
		contentY = py + 34;
		contentW = pw - 112 - 12;
		contentH = ph - 34 - 10;
	}

	private float openProgress() {
		return Draw.easeOut((System.currentTimeMillis() - openedAt) / (float) OPEN_MS);
	}

	@Override
	public void extractBackground(GuiGraphicsExtractor g, int mouseX, int mouseY, float a) {
		g.fill(0, 0, this.width, this.height, Draw.withAlpha(0x0A0818, Math.round(150 * openProgress())));
	}

	@Override
	public void extractRenderState(GuiGraphicsExtractor g, int mouseX, int mouseY, float delta) {
		super.extractRenderState(g, mouseX, mouseY, delta);
		long now = System.currentTimeMillis();
		float dt = lastFrame == 0 ? 0 : Math.min(0.1f, (now - lastFrame) / 1000f);
		lastFrame = now;
		layout();
		int accent = FxConfig.argb(cfg.menuAccent, 0xFFFFC857);
		float open = openProgress();

		g.pose().pushMatrix();
		// gentle zoom-in on open, around the panel centre
		float s = 0.94f + 0.06f * open;
		g.pose().translate(px + pw / 2f, py + ph / 2f);
		g.pose().scale(s);
		g.pose().translate(-(px + pw / 2f), -(py + ph / 2f));

		Draw.roundRect(g, px, py, px + pw, py + ph, 10, Draw.alpha(PANEL, open));
		Draw.roundOutline(g, px, py, px + pw, py + ph, 10, Draw.alpha(LINE, open));

		// header
		g.text(this.font, "✦", px + 12, py + 12, accent, false);
		g.text(this.font, "Nova", px + 24, py + 12, INK, true);
		String hint = "Правый Shift — закрыть";
		g.text(this.font, hint, px + pw - 12 - this.font.width(Component.literal(hint)), py + 12, MUTED, false);

		// sidebar
		Draw.roundRect(g, px + 6, py + 30, px + 104, py + ph - 6, 8, SIDEBAR);
		for (int i = 0; i < tabs.size(); i++) {
			int ty = py + 36 + i * 22;
			boolean hovered = mouseX >= px + 10 && mouseX < px + 100 && mouseY >= ty && mouseY < ty + 18;
			tabAnim[i] = Draw.approach(tabAnim[i], i == tab ? 1f : hovered ? 0.4f : 0f, 14f, dt);
			if (tabAnim[i] > 0.01f) {
				Draw.roundRect(g, px + 10, ty, px + 100, ty + 18, 6, Draw.withAlpha(0x2B2552, Math.round(255 * tabAnim[i])));
			}
			if (i == tab) {
				Draw.roundRect(g, px + 10, ty + 4, px + 12, ty + 14, 1, accent);
			}
			g.text(this.font, tabs.get(i).name(), px + 18, ty + 5, Draw.lerpColor(MUTED, INK, tabAnim[i]), false);
		}

		// content (scrollable)
		List<Opt> opts = tabs.get(tab).opts();
		boolean music = isMusicTab();
		int total = opts.size() * ROW_H + (music ? 16 : 0);
		float maxScroll = Math.max(0, total - contentH);
		scrollTarget = Math.clamp(scrollTarget, 0, maxScroll);
		scroll = Draw.approach(scroll, scrollTarget, 18f, dt);

		g.enableScissor(contentX - 4, contentY, contentX + contentW + 4, contentY + contentH);
		int y = contentY - Math.round(scroll);
		if (music) {
			g.text(this.font, musicStatus(), contentX + 4, y + 3, MUTED, false);
			y += 16;
		}
		for (Opt o : opts) {
			boolean hovered = mouseX >= contentX && mouseX < contentX + contentW && mouseY >= Math.max(y, contentY)
					&& mouseY < Math.min(y + ROW_H, contentY + contentH);
			o.hover = Draw.approach(o.hover, hovered || dragging == o ? 1f : 0f, 14f, dt);
			if (o.hover > 0.01f) {
				Draw.roundRect(g, contentX - 2, y + 1, contentX + contentW + 2, y + ROW_H - 1, 6, Draw.alpha(ROW_HOVER, o.hover));
			}
			g.text(this.font, o.label, contentX + 4, y + 8, INK, false);
			o.draw(this, g, contentX + 4, y, contentW - 8, accent, dt);
			y += ROW_H;
		}
		g.disableScissor();
		g.pose().popMatrix();
	}

	private boolean isMusicTab() {
		return "Музыка".equals(tabs.get(tab).name());
	}

	private static String musicStatus() {
		LyricsState.Data d = LyricsState.data();
		return switch (d.status) {
			case "ok" -> "♪ " + d.artist + " — " + d.title;
			case "searching" -> "Ищу текст: " + d.title;
			case "not_found" -> "Нет синхронного текста: " + d.title;
			default -> "Включите музыку; лаунчер Nova должен быть открыт";
		};
	}

	// ------------------------------------------------------------------ input

	@Override
	public boolean mouseClicked(MouseButtonEvent event, boolean doubleClick) {
		double mx = event.x(), my = event.y();
		layout();
		for (int i = 0; i < tabs.size(); i++) {
			int ty = py + 36 + i * 22;
			if (mx >= px + 10 && mx < px + 100 && my >= ty && my < ty + 18) {
				if (tab != i) {
					tab = i;
					scroll = scrollTarget = 0;
				}
				return true;
			}
		}
		if (mx >= contentX && mx < contentX + contentW && my >= contentY && my < contentY + contentH) {
			int y = contentY - Math.round(scroll) + (isMusicTab() ? 16 : 0);
			for (Opt o : tabs.get(tab).opts()) {
				if (my >= y && my < y + ROW_H) {
					o.click(mx, contentX + 4, contentW - 8);
					return true;
				}
				y += ROW_H;
			}
		}
		return super.mouseClicked(event, doubleClick);
	}

	@Override
	public boolean mouseDragged(MouseButtonEvent event, double dragX, double dragY) {
		if (dragging != null) {
			dragging.setFromMouse(event.x());
			return true;
		}
		return super.mouseDragged(event, dragX, dragY);
	}

	@Override
	public boolean mouseReleased(MouseButtonEvent event) {
		if (dragging != null) {
			dragging = null;
			changed();
			return true;
		}
		return super.mouseReleased(event);
	}

	@Override
	public boolean mouseScrolled(double mouseX, double mouseY, double scrollX, double scrollY) {
		scrollTarget -= (float) (scrollY * 18);
		return true;
	}

	@Override
	public boolean keyPressed(KeyEvent event) {
		if (NovaFX.menuKey() != null && NovaFX.menuKey().matches(event)) {
			onClose();
			return true;
		}
		return super.keyPressed(event);
	}

	@Override
	public void onClose() {
		cfg.save();
		this.minecraft.gui.setScreen(parent);
	}
}
