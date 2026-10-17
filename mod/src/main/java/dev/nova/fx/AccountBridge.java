package dev.nova.fx;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;

import com.google.gson.Gson;
import com.google.gson.JsonObject;
import com.google.gson.JsonParseException;

import net.fabricmc.loader.api.FabricLoader;

/**
 * Talks to the Nova launcher, which keeps the accounts (encrypted) and does the Microsoft login.
 * The launcher writes its local port and key to config/nova-bridge.json.
 */
public final class AccountBridge {
	public static final class Account {
		public String id;
		public String name;
		public String type; // "microsoft" or "offline"
	}

	public static final class Accounts {
		public String selected = "";
		public List<Account> accounts = List.of();
		public boolean microsoft;
	}

	public static final class Session {
		public String id, name, uuid, accessToken, xuid, type;
	}

	public static final class Login {
		public boolean active;
		public String userCode = "";
		public String uri = "";
		public String status = "";
		public String error = "";
		public String accountId = "";
	}

	/** Thrown with a message that can be shown to the player. */
	public static final class BridgeException extends RuntimeException {
		public BridgeException(String message) {
			super(message);
		}
	}

	private static final Gson GSON = new Gson();
	private static final HttpClient HTTP = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(2)).build();

	private AccountBridge() {
	}

	private record Info(int port, String key) {
	}

	private static Info info() {
		Path file = FabricLoader.getInstance().getConfigDir().resolve("nova-bridge.json");
		try {
			JsonObject o = GSON.fromJson(Files.readString(file, StandardCharsets.UTF_8), JsonObject.class);
			return new Info(o.get("port").getAsInt(), o.get("key").getAsString());
		} catch (IOException | JsonParseException | NullPointerException | IllegalStateException e) {
			throw new BridgeException("Лаунчер Nova не запущен: откройте его, чтобы менять аккаунты");
		}
	}

	private static <T> T call(String method, String path, Object body, Class<T> type) {
		Info info = info();
		HttpRequest.Builder req = HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + info.port() + path))
				.timeout(Duration.ofSeconds(70))
				.header("X-Nova-Key", info.key())
				.header("Content-Type", "application/json");
		if ("POST".equals(method)) {
			req.POST(HttpRequest.BodyPublishers.ofString(body == null ? "{}" : GSON.toJson(body), StandardCharsets.UTF_8));
		} else {
			req.GET();
		}
		try {
			HttpResponse<String> resp = HTTP.send(req.build(), HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8));
			if (resp.statusCode() != 200) {
				String msg = "Ошибка лаунчера (" + resp.statusCode() + ")";
				try {
					JsonObject err = GSON.fromJson(resp.body(), JsonObject.class);
					if (err != null && err.has("error")) msg = err.get("error").getAsString();
				} catch (JsonParseException ignored) {
					// keep the generic message
				}
				throw new BridgeException(msg);
			}
			return GSON.fromJson(resp.body(), type);
		} catch (IOException e) {
			throw new BridgeException("Лаунчер Nova не отвечает: перезапустите его");
		} catch (InterruptedException e) {
			Thread.currentThread().interrupt();
			throw new BridgeException("Прервано");
		}
	}

	// All calls run off the render thread.

	public static CompletableFuture<Accounts> accounts() {
		return CompletableFuture.supplyAsync(() -> {
			Accounts a = call("GET", "/v1/accounts", null, Accounts.class);
			if (a.accounts == null) a.accounts = List.of();
			return a;
		});
	}

	public static CompletableFuture<Session> select(String id) {
		return CompletableFuture.supplyAsync(() -> call("POST", "/v1/accounts/select", Map.of("id", id), Session.class));
	}

	public static CompletableFuture<String> addOffline(String name) {
		return CompletableFuture.supplyAsync(() -> call("POST", "/v1/accounts/offline", Map.of("name", name), JsonObject.class).get("id").getAsString());
	}

	public static CompletableFuture<Void> remove(String id) {
		return CompletableFuture.runAsync(() -> call("POST", "/v1/accounts/remove", Map.of("id", id), JsonObject.class));
	}

	public static CompletableFuture<Login> startLogin() {
		return CompletableFuture.supplyAsync(() -> {
			call("POST", "/v1/login/start", null, JsonObject.class);
			return call("GET", "/v1/login/status", null, Login.class);
		});
	}

	public static CompletableFuture<Login> loginStatus() {
		return CompletableFuture.supplyAsync(() -> call("GET", "/v1/login/status", null, Login.class));
	}

	public static void cancelLogin() {
		CompletableFuture.runAsync(() -> {
			try {
				call("POST", "/v1/login/cancel", null, JsonObject.class);
			} catch (BridgeException ignored) {
				// launcher gone: nothing to cancel
			}
		});
	}

	/** Unwraps CompletionException to a readable message. */
	public static String message(Throwable t) {
		while (t.getCause() != null && !(t instanceof BridgeException)) t = t.getCause();
		return t instanceof BridgeException ? t.getMessage() : "Ошибка: " + t.getMessage();
	}
}
