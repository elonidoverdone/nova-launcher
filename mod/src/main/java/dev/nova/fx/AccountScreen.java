package dev.nova.fx;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CompletableFuture;

import net.minecraft.client.gui.GuiGraphicsExtractor;
import net.minecraft.client.gui.components.Button;
import net.minecraft.client.gui.components.EditBox;
import net.minecraft.client.gui.screens.Screen;
import net.minecraft.client.input.MouseButtonEvent;
import net.minecraft.network.chat.Component;

/**
 * Account switcher: pick an account to sign in with right away, add an offline nickname,
 * sign in with Microsoft (code shown here, page opens in the browser), remove accounts.
 * The launcher stores the accounts, so the choice is remembered for the next launch.
 */
public final class AccountScreen extends Screen {
	private static final int PANEL = 0xF2151229;
	private static final int LINE = 0xFF352F61;
	private static final int INK = 0xFFECE9FF;
	private static final int MUTED = 0xFF9A94C4;
	private static final int ROW = 0xFF1E1A3D;
	private static final int ROW_HOVER = 0xFF2B2552;
	private static final int MS_BLUE = 0xFF5CC8FF;
	private static final int OFFLINE_GREEN = 0xFF6FE0A8;
	private static final int ERROR = 0xFFFF6B7A;
	private static final int ROW_H = 26;

	private final Screen parent;
	private final boolean firstRun;
	private final long openedAt = System.currentTimeMillis();

	private List<AccountBridge.Account> accounts = new ArrayList<>();
	private String selected = "";
	private boolean microsoftReady;
	private String status = "Загрузка…";
	private boolean statusError;

	private CompletableFuture<AccountBridge.Accounts> loading;
	private CompletableFuture<AccountBridge.Session> switching;
	private CompletableFuture<AccountBridge.Login> loginPoll;
	private AccountBridge.Login login; // non-null while the Microsoft code is shown
	private long lastLoginPoll;
	private String switchingId = "";

	private EditBox nickField;
	private Button addOfflineBtn;
	private Button microsoftBtn;
	private Button cancelLoginBtn;
	private Button doneBtn;
	private float scroll;

	private int px, py, pw, ph, listX, listY, listW, listH;

	public AccountScreen(Screen parent, boolean firstRun) {
		super(Component.literal("Аккаунты"));
		this.parent = parent;
		this.firstRun = firstRun;
	}

	private void layout() {
		pw = Math.min(320, this.width - 20);
		ph = Math.min(250, this.height - 20);
		px = (this.width - pw) / 2;
		py = (this.height - ph) / 2;
		listX = px + 12;
		listY = py + 42;
		listW = pw - 24;
		listH = ph - 42 - 62;
	}

	@Override
	protected void init() {
		layout();
		int by = py + ph - 52;
		nickField = new EditBox(this.font, px + 12, by, pw - 24 - 108, 20, Component.literal("Ник"));
		nickField.setMaxLength(16);
		nickField.setResponder(v -> addOfflineBtn.active = validNick(v) && switching == null);
		addRenderableWidget(nickField);
		addOfflineBtn = addRenderableWidget(Button.builder(Component.literal("Добавить офлайн"), b -> addOffline())
				.bounds(px + pw - 12 - 104, by, 104, 20).build());
		addOfflineBtn.active = false;

		microsoftBtn = addRenderableWidget(Button.builder(Component.literal("Войти через Microsoft"), b -> startMicrosoft())
				.bounds(px + 12, by + 24, (pw - 24) / 2 - 2, 20).build());
		doneBtn = addRenderableWidget(Button.builder(Component.literal("Готово"), b -> onClose())
				.bounds(px + pw / 2 + 2, by + 24, (pw - 24) / 2 - 2, 20).build());
		cancelLoginBtn = addRenderableWidget(Button.builder(Component.literal("Отменить вход"), b -> cancelMicrosoft())
				.bounds(px + pw / 2 - 60, py + ph - 30, 120, 20).build());
		cancelLoginBtn.visible = false;
		refresh();
	}

	private static boolean validNick(String s) {
		return s.matches("[A-Za-z0-9_]{3,16}");
	}

	private void refresh() {
		loading = AccountBridge.accounts();
	}

	private void setStatus(String s, boolean error) {
		status = s;
		statusError = error;
	}

	private void addOffline() {
		String nick = nickField.getValue().trim();
		if (!validNick(nick)) return;
		if (switching != null) return;
		setStatus("Добавляю " + nick + "…", false);
		switching = AccountBridge.addOffline(nick).thenCompose(AccountBridge::select);
		nickField.setValue("");
	}

	private void select(String id) {
		if (switching != null || login != null) return;
		switchingId = id;
		setStatus("Вход…", false);
		switching = AccountBridge.select(id);
	}

	private void startMicrosoft() {
		if (!microsoftReady) {
			setStatus("Для входа Microsoft укажите Client ID в настройках лаунчера", true);
			return;
		}
		setStatus("Запрашиваю код…", false);
		loginPoll = AccountBridge.startLogin();
	}

	private void cancelMicrosoft() {
		AccountBridge.cancelLogin();
		login = null;
		loginPoll = null;
		setStatus("Вход отменён", false);
	}

	// ------------------------------------------------------------------ async results (render thread)

	private void pump() {
		long now = System.currentTimeMillis();
		if (loading != null && loading.isDone()) {
			try {
				AccountBridge.Accounts a = loading.join();
				accounts = new ArrayList<>(a.accounts);
				selected = a.selected == null ? "" : a.selected;
				microsoftReady = a.microsoft;
				if (status.startsWith("Загрузка")) {
					setStatus(accounts.isEmpty() ? "Добавьте аккаунт, чтобы играть" : "Нажмите на аккаунт, чтобы войти", false);
				}
			} catch (RuntimeException e) {
				setStatus(AccountBridge.message(e), true);
			}
			loading = null;
		}
		if (switching != null && switching.isDone()) {
			try {
				AccountBridge.Session s = switching.join();
				SessionSwitcher.apply(s);
				setStatus("Вы вошли как " + s.name, false);
				selected = s.id;
			} catch (RuntimeException e) {
				setStatus(AccountBridge.message(e), true);
			}
			switching = null;
			switchingId = "";
			refresh();
		}
		if (loginPoll != null && loginPoll.isDone()) {
			try {
				AccountBridge.Login l = loginPoll.join();
				if (l.accountId != null && !l.accountId.isEmpty()) {
					login = null;
					select(l.accountId);
				} else if (l.error != null && !l.error.isEmpty()) {
					login = null;
					setStatus(l.error, true);
				} else if (l.active) {
					login = l;
				} else {
					login = null;
				}
			} catch (RuntimeException e) {
				login = null;
				setStatus(AccountBridge.message(e), true);
			}
			loginPoll = null;
		}
		if (login != null && loginPoll == null && now - lastLoginPoll > 1000) {
			lastLoginPoll = now;
			loginPoll = AccountBridge.loginStatus();
		}

		boolean loginMode = login != null;
		nickField.visible = !loginMode;
		addOfflineBtn.visible = !loginMode;
		addOfflineBtn.active = validNick(nickField.getValue()) && switching == null;
		microsoftBtn.visible = !loginMode;
		microsoftBtn.active = switching == null && loginPoll == null;
		doneBtn.visible = !loginMode;
		cancelLoginBtn.visible = loginMode;
	}

	// ------------------------------------------------------------------ drawing

	@Override
	public void extractBackground(GuiGraphicsExtractor g, int mouseX, int mouseY, float a) {
		layout();
		float open = Draw.easeOut((System.currentTimeMillis() - openedAt) / 200f);
		g.fill(0, 0, this.width, this.height, Draw.withAlpha(0x0A0818, Math.round(170 * open)));
		Draw.roundRect(g, px, py, px + pw, py + ph, 10, PANEL);
		Draw.roundOutline(g, px, py, px + pw, py + ph, 10, LINE);
	}

	@Override
	public void extractRenderState(GuiGraphicsExtractor g, int mouseX, int mouseY, float delta) {
		pump();
		super.extractRenderState(g, mouseX, mouseY, delta);
		int accent = FxConfig.argb(NovaFX.config().menuAccent, 0xFFFFC857);

		g.text(this.font, "✦", px + 12, py + 12, accent, false);
		g.text(this.font, firstRun ? "Вход в Nova" : "Аккаунты", px + 24, py + 12, INK, true);
		String now = "Сейчас: " + this.minecraft.getUser().getName();
		g.text(this.font, now, px + pw - 12 - this.font.width(Component.literal(now)), py + 12, MUTED, false);
		g.text(this.font, status, px + 12, py + 28, statusError ? ERROR : MUTED, false);

		if (login != null) {
			drawLogin(g, accent);
			return;
		}

		float maxScroll = Math.max(0, accounts.size() * ROW_H - listH);
		scroll = Math.clamp(scroll, 0, maxScroll);
		g.enableScissor(listX, listY, listX + listW, listY + listH);
		int y = listY - Math.round(scroll);
		for (AccountBridge.Account acc : accounts) {
			boolean hover = mouseX >= listX && mouseX < listX + listW && mouseY >= Math.max(y, listY) && mouseY < Math.min(y + ROW_H - 2, listY + listH);
			boolean current = acc.id.equals(selected);
			Draw.roundRect(g, listX, y, listX + listW, y + ROW_H - 2, 6, hover ? ROW_HOVER : ROW);
			if (current) Draw.roundRect(g, listX, y + 6, listX + 2, y + ROW_H - 8, 1, accent);

			// letter avatar
			boolean ms = "microsoft".equals(acc.type);
			int av = ms ? MS_BLUE : OFFLINE_GREEN;
			Draw.roundRect(g, listX + 6, y + 4, listX + 22, y + 20, 4, Draw.withAlpha(av, 70));
			String letter = acc.name.isEmpty() ? "?" : acc.name.substring(0, 1).toUpperCase();
			g.text(this.font, letter, listX + 14 - this.font.width(Component.literal(letter)) / 2, y + 8, av, false);

			g.text(this.font, acc.name, listX + 28, y + 4, INK, false);
			String tag = (ms ? "Microsoft" : "Офлайн") + (current ? "  •  в игре" : "");
			if (acc.id.equals(switchingId)) tag = "вход…";
			g.text(this.font, tag, listX + 28, y + 14, MUTED, false);

			if (hover) {
				g.text(this.font, "✕", listX + listW - 14, y + 9, ERROR, false);
			}
			y += ROW_H;
		}
		if (accounts.isEmpty() && loading == null) {
			g.text(this.font, "Аккаунтов пока нет", listX + 4, listY + 6, MUTED, false);
		}
		g.disableScissor();
	}

	private void drawLogin(GuiGraphicsExtractor g, int accent) {
		int cy = py + 60;
		String l1 = "Страница Microsoft открылась в браузере.";
		String l2 = "Введите там этот код:";
		g.text(this.font, l1, px + (pw - this.font.width(Component.literal(l1))) / 2, cy, INK, false);
		g.text(this.font, l2, px + (pw - this.font.width(Component.literal(l2))) / 2, cy + 12, MUTED, false);
		String code = login.userCode == null ? "" : login.userCode;
		g.pose().pushMatrix();
		float scale = 2.4f;
		g.pose().translate(px + pw / 2f, cy + 40);
		g.pose().scale(scale);
		g.text(this.font, code, -this.font.width(Component.literal(code)) / 2, -4, accent, true);
		g.pose().popMatrix();
		String uri = login.uri == null ? "" : login.uri;
		g.text(this.font, uri, px + (pw - this.font.width(Component.literal(uri))) / 2, cy + 66, MUTED, false);
		String st = login.status == null ? "" : login.status;
		g.text(this.font, st, px + (pw - this.font.width(Component.literal(st))) / 2, cy + 82, INK, false);
	}

	// ------------------------------------------------------------------ input

	@Override
	public boolean mouseClicked(MouseButtonEvent event, boolean doubleClick) {
		double mx = event.x(), my = event.y();
		if (login == null && mx >= listX && mx < listX + listW && my >= listY && my < listY + listH) {
			int i = (int) ((my - listY + scroll) / ROW_H);
			if (i >= 0 && i < accounts.size()) {
				AccountBridge.Account acc = accounts.get(i);
				if (mx >= listX + listW - 20) {
					setStatus("Удалён " + acc.name, false);
					AccountBridge.remove(acc.id).whenComplete((v, e) -> refresh());
				} else if (!acc.id.equals(selected)) {
					select(acc.id);
				}
				return true;
			}
		}
		return super.mouseClicked(event, doubleClick);
	}

	@Override
	public boolean mouseScrolled(double mouseX, double mouseY, double scrollX, double scrollY) {
		scroll -= (float) (scrollY * 14);
		return true;
	}

	@Override
	public void onClose() {
		if (login != null) AccountBridge.cancelLogin();
		this.minecraft.gui.setScreen(parent);
	}
}
