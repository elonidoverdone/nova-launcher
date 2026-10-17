package dev.nova.fx;

import java.util.concurrent.CompletableFuture;

import net.minecraft.client.Minecraft;
import net.minecraft.client.gui.GuiGraphicsExtractor;
import net.minecraft.client.gui.screens.Screen;
import net.minecraft.network.chat.Component;

import net.fabricmc.fabric.api.client.screen.v1.ScreenEvents;
import net.fabricmc.fabric.api.client.screen.v1.ScreenMouseEvents;

/**
 * The current account in the top-right corner of the title and multiplayer screens.
 * Clicking it opens the account switcher. On the first title screen after start, if the
 * launcher has no accounts yet, the sign-in screen opens by itself.
 */
public final class AccountBadge {
	private static boolean firstTitle = true;
	private static CompletableFuture<AccountBridge.Accounts> firstCheck;
	private static String type = ""; // type of the account in use, from the launcher

	private AccountBadge() {
	}

	private static int[] bounds(Screen s, Minecraft mc) {
		String name = mc.getUser().getName();
		int w = 30 + mc.font.width(Component.literal(name)) + 14;
		int x2 = s.width - 6;
		return new int[] {x2 - w, 6, x2, 26};
	}

	/** Called from ScreenEvents.BEFORE_INIT for the title and multiplayer screens. */
	public static void attach(Minecraft client, Screen screen, boolean title) {
		if (title && firstTitle) {
			firstTitle = false;
			firstCheck = AccountBridge.accounts();
		}
		// learn the account type for the badge (cheap local call)
		AccountBridge.accounts().thenAccept(a -> a.accounts.stream()
				.filter(acc -> acc.id.equals(a.selected)).findFirst()
				.ifPresent(acc -> type = acc.type)).exceptionally(e -> null);

		ScreenEvents.afterExtract(screen).register((s, g, mouseX, mouseY, tick) -> {
			if (firstCheck != null && firstCheck.isDone()) {
				CompletableFuture<AccountBridge.Accounts> done = firstCheck;
				firstCheck = null;
				try {
					if (done.join().accounts.isEmpty()) {
						client.gui.setScreen(new AccountScreen(s, true));
						return;
					}
				} catch (RuntimeException ignored) {
					// launcher not running: just show the badge
				}
			}
			draw(g, s, client, mouseX, mouseY);
		});
		ScreenMouseEvents.allowMouseClick(screen).register((s, event) -> {
			int[] b = bounds(s, client);
			if (event.x() >= b[0] && event.x() < b[2] && event.y() >= b[1] && event.y() < b[3]) {
				client.gui.setScreen(new AccountScreen(s, false));
				return false;
			}
			return true;
		});
	}

	private static void draw(GuiGraphicsExtractor g, Screen s, Minecraft mc, int mouseX, int mouseY) {
		int[] b = bounds(s, mc);
		boolean hover = mouseX >= b[0] && mouseX < b[2] && mouseY >= b[1] && mouseY < b[3];
		Draw.roundRect(g, b[0], b[1], b[2], b[3], 10, hover ? 0xE62B2552 : 0xCC151229);
		Draw.roundOutline(g, b[0], b[1], b[2], b[3], 10, 0xFF352F61);

		boolean ms = "microsoft".equals(type);
		int av = ms ? 0xFF5CC8FF : 0xFF6FE0A8;
		Draw.roundRect(g, b[0] + 4, b[1] + 3, b[0] + 18, b[1] + 17, 7, Draw.withAlpha(av, 80));
		String name = mc.getUser().getName();
		String letter = name.isEmpty() ? "?" : name.substring(0, 1).toUpperCase();
		g.text(mc.font, letter, b[0] + 11 - mc.font.width(Component.literal(letter)) / 2, b[1] + 6, av, false);
		g.text(mc.font, name, b[0] + 24, b[1] + 6, 0xFFECE9FF, false);
		g.text(mc.font, "▾", b[2] - 11, b[1] + 6, 0xFF9A94C4, false);
	}
}
