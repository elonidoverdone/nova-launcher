// Adapted from Auth Me by Axieum (https://github.com/axieum/authme), MIT License.
// See THIRD_PARTY_NOTICES.txt in the mod folder.
package dev.nova.fx.mixin;

import java.net.Proxy;
import java.util.concurrent.CompletableFuture;

import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.Mutable;
import org.spongepowered.asm.mixin.gen.Accessor;

import com.mojang.authlib.minecraft.UserApiService;
import com.mojang.authlib.services.ProfileResult;
import com.mojang.realmsclient.gui.RealmsDataFetcher;

import net.minecraft.client.Minecraft;
import net.minecraft.client.User;
import net.minecraft.client.gui.screens.social.PlayerSocialManager;
import net.minecraft.client.gui.screens.social.RemoteFriendListUpdateHandler;
import net.minecraft.client.multiplayer.ProfileKeyPairManager;
import net.minecraft.client.multiplayer.chat.report.ReportingContext;

/** Lets NovaFX replace the signed-in user while the game is running. */
@Mixin(Minecraft.class)
public interface MinecraftAccessor {
	@Accessor
	@Mutable
	void setOfflineDeveloperMode(boolean offlineDeveloperMode);

	@Accessor
	Proxy getProxy();

	@Accessor
	@Mutable
	void setUser(User user);

	@Accessor
	@Mutable
	void setProfileFuture(CompletableFuture<ProfileResult> future);

	@Accessor
	@Mutable
	void setUserApiService(UserApiService userApiService);

	@Accessor
	@Mutable
	void setPlayerSocialManager(PlayerSocialManager playerSocialManager);

	@Accessor
	RemoteFriendListUpdateHandler getRemoteFriendListUpdateHandler();

	@Accessor
	@Mutable
	void setRemoteFriendListUpdateHandler(RemoteFriendListUpdateHandler handler);

	@Accessor
	@Mutable
	void setProfileKeyPairManager(ProfileKeyPairManager profileKeyPairManager);

	@Accessor
	@Mutable
	void setReportingContext(ReportingContext reportingContext);

	@Accessor
	@Mutable
	void setRealmsDataFetcher(RealmsDataFetcher realmsDataFetcher);
}
