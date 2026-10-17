// Adapted from Auth Me by Axieum (https://github.com/axieum/authme), MIT License.
// See THIRD_PARTY_NOTICES.txt in the mod folder.
package dev.nova.fx.mixin;

import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.Mutable;
import org.spongepowered.asm.mixin.gen.Accessor;

import net.minecraft.client.User;
import net.minecraft.client.resources.SplashManager;

@Mixin(SplashManager.class)
public interface SplashManagerAccessor {
	@Accessor
	@Mutable
	void setUser(User user);
}
