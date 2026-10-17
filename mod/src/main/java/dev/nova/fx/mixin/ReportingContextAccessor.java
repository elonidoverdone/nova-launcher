// Adapted from Auth Me by Axieum (https://github.com/axieum/authme), MIT License.
// See THIRD_PARTY_NOTICES.txt in the mod folder.
package dev.nova.fx.mixin;

import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.gen.Accessor;

import net.minecraft.client.multiplayer.chat.report.ReportEnvironment;
import net.minecraft.client.multiplayer.chat.report.ReportingContext;

@Mixin(ReportingContext.class)
public interface ReportingContextAccessor {
	@Accessor
	ReportEnvironment getEnvironment();
}
