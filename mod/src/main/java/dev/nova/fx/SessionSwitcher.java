// Session switching adapted from Auth Me by Axieum (https://github.com/axieum/authme), MIT License.
// See THIRD_PARTY_NOTICES.txt in the mod folder.
package dev.nova.fx;

import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;

import com.mojang.authlib.minecraft.UserApiService;
import com.mojang.authlib.services.FriendsService;
import com.mojang.authlib.services.MinecraftServicesDiscoveryService;
import com.mojang.realmsclient.client.RealmsClient;
import com.mojang.realmsclient.gui.RealmsDataFetcher;

import net.minecraft.client.Minecraft;
import net.minecraft.client.User;
import net.minecraft.client.gui.screens.social.PlayerSocialManager;
import net.minecraft.client.gui.screens.social.RemoteFriendListUpdateHandler;
import net.minecraft.client.multiplayer.ProfileKeyPairManager;
import net.minecraft.client.multiplayer.chat.report.ReportingContext;
import net.minecraft.util.Util;

import dev.nova.fx.mixin.MinecraftAccessor;
import dev.nova.fx.mixin.RealmsAvailabilityAccessor;
import dev.nova.fx.mixin.RealmsClientAccessor;
import dev.nova.fx.mixin.ReportingContextAccessor;
import dev.nova.fx.mixin.SplashManagerAccessor;

/** Replaces the signed-in user of the running game (must be called on the render thread). */
public final class SessionSwitcher {
	private SessionSwitcher() {
	}

	/** Parses "0123abcd..." or a dashed UUID. */
	static UUID parseUuid(String s) {
		String hex = s.replace("-", "");
		if (hex.length() != 32) throw new IllegalArgumentException("bad uuid " + s);
		return new UUID(Long.parseUnsignedLong(hex.substring(0, 16), 16), Long.parseUnsignedLong(hex.substring(16), 16));
	}

	public static void apply(AccountBridge.Session s) {
		boolean offline = !"microsoft".equals(s.type) || s.accessToken == null || s.accessToken.isBlank() || "0".equals(s.accessToken);
		User user = new User(
				s.name,
				parseUuid(s.uuid),
				offline ? "0" : s.accessToken,
				Optional.ofNullable(s.xuid == null || s.xuid.isBlank() || "0".equals(s.xuid) ? null : s.xuid),
				Optional.empty());
		apply(user, offline);
	}

	public static void apply(User user, boolean offline) {
		Minecraft client = Minecraft.getInstance();
		MinecraftAccessor acc = (MinecraftAccessor) client;

		acc.setUser(user);
		((SplashManagerAccessor) client.gui.splashManager()).setUser(user);
		acc.setProfileFuture(CompletableFuture.supplyAsync(
				() -> client.services().sessionService().fetchProfile(user.getProfileId(), true),
				Util.nonCriticalIoPool()));
		acc.setOfflineDeveloperMode(offline);

		MinecraftServicesDiscoveryService discovery = MinecraftServicesDiscoveryService.create(acc.getProxy(), !offline);
		UserApiService userApi = offline ? UserApiService.OFFLINE : discovery.createUserApiService(user.getAccessToken());
		acc.setUserApiService(userApi);

		acc.getRemoteFriendListUpdateHandler().stop();
		FriendsService friends = discovery.createFriendsService(user.getAccessToken());
		RemoteFriendListUpdateHandler friendUpdates = new RemoteFriendListUpdateHandler(friends, client);
		acc.setRemoteFriendListUpdateHandler(friendUpdates);
		PlayerSocialManager social = new PlayerSocialManager(client, userApi, friends, friendUpdates);
		acc.setPlayerSocialManager(social);
		if (social.isFriendListEnabled()) {
			friendUpdates.start();
		}

		// chat signing keys and abuse reports belong to the account
		acc.setProfileKeyPairManager(ProfileKeyPairManager.create(userApi, user, client.gameDirectory.toPath()));
		acc.setReportingContext(ReportingContext.create(
				((ReportingContextAccessor) (Object) client.getReportingContext()).getEnvironment(), userApi));

		// Realms keeps its own copy of the session
		synchronized (RealmsClient.class) {
			RealmsClient realms = RealmsClientAccessor.create(user.getSessionId(), user.getName(), client);
			RealmsClientAccessor.setRealmsClientInstance(realms);
			acc.setRealmsDataFetcher(new RealmsDataFetcher(realms));
			RealmsAvailabilityAccessor.setFuture(null);
		}
		NovaFX.LOG.info("[NovaFX] signed in as {} ({})", user.getName(), offline ? "offline" : "Microsoft");
	}
}
