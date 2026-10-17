// Adapted from Auth Me by Axieum (https://github.com/axieum/authme), MIT License.
// See THIRD_PARTY_NOTICES.txt in the mod folder.
package dev.nova.fx.mixin;

import java.util.concurrent.CompletableFuture;

import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.Mutable;
import org.spongepowered.asm.mixin.gen.Accessor;

import com.mojang.realmsclient.RealmsAvailability;

@Mixin(RealmsAvailability.class)
public interface RealmsAvailabilityAccessor {
	@Accessor
	@Mutable
	static void setFuture(CompletableFuture<RealmsAvailability.Result> future) {
		throw new AssertionError();
	}
}
