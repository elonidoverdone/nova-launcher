package dev.nova.fx;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonParseException;

import net.fabricmc.loader.api.FabricLoader;

/**
 * Settings stored in config/novafx.json. The launcher edits the same file,
 * and the mod picks up changes while the game is running.
 */
public final class FxConfig {
	/** Built-in particle styles the hotkey cycles through (any vanilla simple particle id also works). */
	public static final List<String> STYLES = List.of(
			"minecraft:crit",
			"minecraft:enchanted_hit",
			"minecraft:heart",
			"minecraft:flame",
			"minecraft:soul_fire_flame",
			"minecraft:electric_spark",
			"minecraft:end_rod",
			"minecraft:totem_of_undying",
			"minecraft:snowflake",
			"minecraft:firework"
	);

	public boolean enabled = true;
	/** Particle id, e.g. "minecraft:flame". */
	public String hitParticle = "minecraft:crit";
	/** BURST, RING or SPIRAL. */
	public String hitShape = "BURST";
	public int particleCount = 14;
	/** Particle speed multiplier, 0.1 .. 3. */
	public float particleSpeed = 1.0f;
	/** Only show effects when hitting players. */
	public boolean onlyPlayers = false;

	/** NONE, PILLAR, EXPLOSION, TOTEM, SOULS. */
	public String killEffect = "PILLAR";

	/** Sound id or "none", e.g. "minecraft:block.amethyst_block.chime". */
	public String hitSound = "none";
	public float soundVolume = 0.8f;
	public float soundPitch = 1.2f;
	public String killSound = "minecraft:entity.experience_orb.pickup";

	public boolean hitMarker = true;
	/** #RRGGBB */
	public String hitMarkerColor = "#FFFFFF";
	public String killMarkerColor = "#FF4D5E";

	// --- song lyrics overlay (data comes from the launcher, see LyricsState) ---
	public boolean lyricsEnabled = true;
	/** Each new line glides to a random spot on screen; off = fixed spot at the top right. */
	public boolean lyricsRandomPos = true;
	public float lyricsScale = 1.6f;
	public String lyricsColor = "#FFFFFF";
	public boolean lyricsBackground = true;
	/** Background opacity, 0..1. */
	public float lyricsBgOpacity = 0.55f;
	/** Letter-by-letter speed multiplier, 0.5 (slow) .. 3 (fast). */
	public float lyricsTypeSpeed = 1.0f;
	/** Shifts lyrics in time if they run early/late, in ms (positive = later). */
	public int lyricsOffsetMs = 0;
	/** Volumetric letters: depth, card-like turn-in, own spot and tilt for each letter, gentle bobbing. */
	public boolean lyrics3D = true;
	/** How far letters wander from the straight line, 0..1. */
	public float lyricsScatter = 0.6f;

	// --- target marker: spinning star on the player/mob under the crosshair that you can hit ---
	public boolean targetEnabled = true;
	/** HEXAGRAM, STAR, DIAMOND, RING */
	public String targetShape = "HEXAGRAM";
	public String targetColor = "#3D7BFF";
	public float targetSize = 1.0f;
	/** Turns per second. */
	public float targetSpeed = 0.6f;
	/** Spin direction in the screen plane: RIGHT = clockwise, LEFT = counter-clockwise. */
	public String targetDirection = "RIGHT";
	public boolean targetGlow = true;
	public boolean targetOnlyPlayers = false;

	/** Accent colour of the in-game menu. */
	public String menuAccent = "#FFC857";

	private static final Gson GSON = new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create();
	private static long lastModified = -1;

	public static Path path() {
		return FabricLoader.getInstance().getConfigDir().resolve("novafx.json");
	}

	/** Loads the config, creating it with defaults if missing. Never throws. */
	public static FxConfig load() {
		Path file = path();
		try {
			if (Files.notExists(file)) {
				FxConfig fresh = new FxConfig();
				fresh.save();
				return fresh;
			}
			lastModified = Files.getLastModifiedTime(file).toMillis();
			FxConfig cfg = GSON.fromJson(Files.readString(file, StandardCharsets.UTF_8), FxConfig.class);
			return cfg == null ? new FxConfig() : cfg.sanitize();
		} catch (IOException | JsonParseException e) {
			NovaFX.LOG.warn("[NovaFX] Could not read {}: {}. Using defaults.", file, e.getMessage());
			return new FxConfig();
		}
	}

	/** True when the file changed on disk since the last load (e.g. the launcher saved new settings). */
	public static boolean changedOnDisk() {
		try {
			Path file = path();
			return Files.exists(file) && Files.getLastModifiedTime(file).toMillis() != lastModified;
		} catch (IOException e) {
			return false;
		}
	}

	public void save() {
		Path file = path();
		try {
			Files.createDirectories(file.getParent());
			Files.writeString(file, GSON.toJson(this), StandardCharsets.UTF_8);
			lastModified = Files.getLastModifiedTime(file).toMillis();
		} catch (IOException e) {
			NovaFX.LOG.warn("[NovaFX] Could not save {}: {}", file, e.getMessage());
		}
	}

	private FxConfig sanitize() {
		particleCount = Math.clamp(particleCount, 1, 80);
		particleSpeed = Math.clamp(particleSpeed, 0.1f, 3.0f);
		soundVolume = Math.clamp(soundVolume, 0.0f, 2.0f);
		soundPitch = Math.clamp(soundPitch, 0.5f, 2.0f);
		if (hitParticle == null || hitParticle.isBlank()) hitParticle = "minecraft:crit";
		if (hitShape == null) hitShape = "BURST";
		if (killEffect == null) killEffect = "NONE";
		if (hitSound == null) hitSound = "none";
		if (killSound == null) killSound = "none";
		if (hitMarkerColor == null) hitMarkerColor = "#FFFFFF";
		if (killMarkerColor == null) killMarkerColor = "#FF4D5E";
		lyricsScale = Math.clamp(lyricsScale, 0.5f, 4.0f);
		lyricsBgOpacity = Math.clamp(lyricsBgOpacity, 0.0f, 1.0f);
		lyricsTypeSpeed = Math.clamp(lyricsTypeSpeed, 0.5f, 3.0f);
		lyricsOffsetMs = Math.clamp(lyricsOffsetMs, -5000, 5000);
		if (lyricsColor == null) lyricsColor = "#FFFFFF";
		if (menuAccent == null) menuAccent = "#FFC857";
		lyricsScatter = Math.clamp(lyricsScatter, 0.0f, 1.0f);
		targetSize = Math.clamp(targetSize, 0.3f, 3.0f);
		targetSpeed = Math.clamp(targetSpeed, 0.0f, 3.0f);
		if (targetShape == null) targetShape = "HEXAGRAM";
		if (targetColor == null) targetColor = "#3D7BFF";
		if (!"LEFT".equalsIgnoreCase(targetDirection)) targetDirection = "RIGHT";
		return this;
	}

	/** Parses #RRGGBB into an opaque ARGB int. */
	public static int argb(String hex, int fallback) {
		if (hex == null) return fallback;
		String h = hex.startsWith("#") ? hex.substring(1) : hex;
		if (h.length() != 6) return fallback;
		try {
			return 0xFF000000 | Integer.parseInt(h, 16);
		} catch (NumberFormatException e) {
			return fallback;
		}
	}
}
