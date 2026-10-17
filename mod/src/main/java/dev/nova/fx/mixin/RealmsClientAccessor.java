// Adapted from Auth Me by Axieum (https://github.com/axieum/authme), MIT License.
// See THIRD_PARTY_NOTICES.txt in the mod folder.
package dev.nova.fx.mixin;

import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.gen.Accessor;
import org.spongepowered.asm.mixin.gen.Invoker;

import com.mojang.realmsclient.client.RealmsClient;

import net.minecraft.client.Minecraft;

@Mixin(RealmsClient.class)
public interface RealmsClientAccessor {
	@Invoker(value = "<init>")
	static RealmsClient create(String sessionId, String username, Minecraft minecraft) {
		throw new AssertionError();
	}

	@Accessor(value = "realmsClientInstance")
	static void setRealmsClientInstance(RealmsClient client) {
		throw new AssertionError();
	}
}
