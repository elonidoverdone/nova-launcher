package dev.nova.fx;

import java.util.Locale;

import com.mojang.blaze3d.platform.InputConstants;
import org.lwjgl.sdl.SDLScancode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import net.minecraft.client.KeyMapping;
import net.minecraft.client.Minecraft;
import net.minecraft.client.gui.screens.PauseScreen;
import net.minecraft.client.gui.screens.TitleScreen;
import net.minecraft.client.gui.screens.multiplayer.JoinMultiplayerScreen;
import net.minecraft.network.chat.Component;
import net.minecraft.resources.Identifier;
import net.minecraft.world.InteractionResult;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.player.Player;

import net.fabricmc.api.ClientModInitializer;
import net.fabricmc.fabric.api.client.event.lifecycle.v1.ClientTickEvents;
import net.fabricmc.fabric.api.client.keymapping.v1.KeyMappingHelper;
import net.fabricmc.fabric.api.client.rendering.v1.hud.HudElementRegistry;
import net.fabricmc.fabric.api.client.rendering.v1.hud.VanillaHudElements;
import net.fabricmc.fabric.api.client.screen.v1.ScreenEvents;
import net.fabricmc.fabric.api.client.screen.v1.ScreenKeyboardEvents;
import net.fabricmc.fabric.api.event.player.AttackEntityCallback;

public final class NovaFX implements ClientModInitializer {
	public static final String MOD_ID = "novafx";
	public static final Logger LOG = LoggerFactory.getLogger("NovaFX");

	private static FxConfig config = new FxConfig();

	/** How long after a hit a death still counts as "our" kill. */
	private static final int KILL_WINDOW_TICKS = 40;

	private final HitMarkerHud hitMarker = new HitMarkerHud();
	private final LyricsHud lyricsHud = new LyricsHud();
	private final TargetMarkerHud targetMarker = new TargetMarkerHud();
	private Entity lastTarget;
	private int lastTargetTicks;
	private int ticks;

	private KeyMapping toggleKey;
	private KeyMapping styleKey;
	private static KeyMapping menuKey;

	public static FxConfig config() {
		return config;
	}

	/** Right Shift by default; rebindable in Controls. */
	public static KeyMapping menuKey() {
		return menuKey;
	}

	@Override
	public void onInitializeClient() {
		config = FxConfig.load();

		KeyMapping.Category category = KeyMapping.Category.register(Identifier.fromNamespaceAndPath(MOD_ID, "main"));
		toggleKey = KeyMappingHelper.registerKeyMapping(new KeyMapping(
				"key.novafx.toggle", InputConstants.Type.KEYBOARD, SDLScancode.SDL_SCANCODE_H, category));
		styleKey = KeyMappingHelper.registerKeyMapping(new KeyMapping(
				"key.novafx.next_style", InputConstants.Type.KEYBOARD, SDLScancode.SDL_SCANCODE_J, category));
		menuKey = KeyMappingHelper.registerKeyMapping(new KeyMapping(
				"key.novafx.menu", InputConstants.Type.KEYBOARD, SDLScancode.SDL_SCANCODE_RSHIFT, category));

		// the menu also opens from the title screen (right after the game starts) and the pause menu
		ScreenEvents.BEFORE_INIT.register((client, screen, w, h) -> {
			// current account in the corner; click to switch
			if (screen instanceof TitleScreen || screen instanceof JoinMultiplayerScreen) {
				AccountBadge.attach(client, screen, screen instanceof TitleScreen);
			}
			if (!(screen instanceof TitleScreen) && !(screen instanceof PauseScreen)) return;
			ScreenKeyboardEvents.beforeKeyPress(screen).register((s, keyEvent) -> {
				if (menuKey.matches(keyEvent)) {
					client.gui.setScreen(new NovaMenuScreen(s));
				}
			});
			if (screen instanceof TitleScreen) {
				ScreenEvents.afterExtract(screen).register((s, graphics, mouseX, mouseY, tick) -> {
					String hint = "\u2726 Nova: \u043f\u0440\u0430\u0432\u044b\u0439 Shift \u2014 \u043c\u0435\u043d\u044e";
					graphics.text(client.font, hint, 4, 4, 0xCCFFC857, true);
				});
			}
		});

		// Fires on the client when the local player attacks (before the packet is sent).
		AttackEntityCallback.EVENT.register((player, level, hand, entity, hitResult) -> {
			if (level.isClientSide() && config.enabled && (!config.onlyPlayers || entity instanceof Player)) {
				HitEffects.onHit(level, entity, config);
				hitMarker.hit();
				targetMarker.hit();
				lastTarget = entity;
				lastTargetTicks = KILL_WINDOW_TICKS;
			}
			return InteractionResult.PASS;
		});

		ClientTickEvents.END_CLIENT_TICK.register(this::onTick);

		HudElementRegistry.attachElementAfter(VanillaHudElements.CROSSHAIR,
				Identifier.fromNamespaceAndPath(MOD_ID, "hit_marker"), hitMarker);
		HudElementRegistry.attachElementAfter(VanillaHudElements.CROSSHAIR,
				Identifier.fromNamespaceAndPath(MOD_ID, "target_marker"), targetMarker);
		HudElementRegistry.addLast(Identifier.fromNamespaceAndPath(MOD_ID, "lyrics"), lyricsHud);

		LOG.info("[NovaFX] loaded, config: {}", FxConfig.path());
	}

	private void onTick(Minecraft client) {
		ticks++;
		hitMarker.tick();

		// pick up settings saved by the launcher while the game is running
		if (ticks % 20 == 0 && FxConfig.changedOnDisk()) {
			config = FxConfig.load();
		}
		if (ticks % 4 == 0) {
			LyricsState.poll();
		}

		while (toggleKey.consumeClick()) {
			config.enabled = !config.enabled;
			config.save();
			say(client, "NovaFX: " + (config.enabled ? "ON" : "OFF"));
		}
		while (menuKey.consumeClick()) {
			client.gui.setScreen(new NovaMenuScreen(null));
		}
		while (styleKey.consumeClick()) {
			int i = FxConfig.STYLES.indexOf(config.hitParticle.toLowerCase(Locale.ROOT));
			config.hitParticle = FxConfig.STYLES.get((i + 1) % FxConfig.STYLES.size());
			config.save();
			say(client, "NovaFX: " + config.hitParticle.replace("minecraft:", ""));
		}

		if (lastTarget != null) {
			boolean dead = lastTarget instanceof LivingEntity living ? living.isDeadOrDying() : lastTarget.isRemoved();
			if (dead && client.level != null) {
				if (config.enabled) {
					HitEffects.onKill(client.level, lastTarget, config);
					hitMarker.kill();
				}
				lastTarget = null;
			} else if (--lastTargetTicks <= 0) {
				lastTarget = null;
			}
		}
	}

	private static void say(Minecraft client, String text) {
		if (client.player != null) {
			client.player.sendOverlayMessage(Component.literal(text));
		}
	}
}
