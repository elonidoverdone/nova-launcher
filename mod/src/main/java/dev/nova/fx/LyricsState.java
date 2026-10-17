package dev.nova.fx;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

import com.google.gson.Gson;
import com.google.gson.JsonParseException;

import net.fabricmc.loader.api.FabricLoader;

/**
 * Reads config/novafx-lyrics.json, which the launcher rewrites whenever the track,
 * play state or position changes. The current line is computed here every frame,
 * so the overlay stays smooth between file updates.
 *
 * File format:
 * { "status": "ok|not_found|idle", "title": "...", "artist": "...", "playing": true,
 *   "positionMs": 12345, "capturedAt": 1760000000000, "lines": [ {"t": 1200, "text": "..."} ] }
 */
public final class LyricsState {
	public static final class Line {
		public long t;
		public String text;
	}

	public static final class Data {
		public String status = "idle";
		public String title = "";
		public String artist = "";
		public boolean playing;
		public long positionMs;
		public long capturedAt;
		public List<Line> lines = List.of();
	}

	private static final Gson GSON = new Gson();
	private static Data data = new Data();
	private static long lastModified = -1;

	private LyricsState() {
	}

	public static Path path() {
		return FabricLoader.getInstance().getConfigDir().resolve("novafx-lyrics.json");
	}

	/** Called a few times per second from the client tick. */
	public static void poll() {
		Path file = path();
		try {
			if (Files.notExists(file)) {
				data = new Data();
				lastModified = -1;
				return;
			}
			long mod = Files.getLastModifiedTime(file).toMillis();
			if (mod == lastModified) return;
			lastModified = mod;
			Data d = GSON.fromJson(Files.readString(file, StandardCharsets.UTF_8), Data.class);
			if (d != null) {
				if (d.lines == null) d.lines = List.of();
				data = d;
			}
		} catch (IOException | JsonParseException e) {
			// the launcher may be mid-write; try again next poll
			lastModified = -1;
		}
	}

	public static Data data() {
		return data;
	}

	/** Current playback position in ms, extrapolated from the last snapshot. */
	public static long positionMs(int offsetMs) {
		Data d = data;
		long pos = d.positionMs;
		if (d.playing && d.capturedAt > 0) {
			pos += System.currentTimeMillis() - d.capturedAt;
		}
		return pos - offsetMs;
	}

	/** Index of the line being sung now, or -1 before the first line. */
	public static int activeIndex(long pos) {
		List<Line> lines = data.lines;
		int lo = 0;
		int hi = lines.size() - 1;
		int ans = -1;
		while (lo <= hi) {
			int mid = (lo + hi) >>> 1;
			if (lines.get(mid).t <= pos) {
				ans = mid;
				lo = mid + 1;
			} else {
				hi = mid - 1;
			}
		}
		return ans;
	}
}
