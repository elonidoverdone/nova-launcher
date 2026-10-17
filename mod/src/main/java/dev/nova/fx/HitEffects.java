package dev.nova.fx;

import java.util.Locale;
import java.util.Optional;
import java.util.concurrent.ThreadLocalRandom;

import net.minecraft.core.particles.ParticleOptions;
import net.minecraft.core.particles.ParticleType;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.sounds.SoundEvent;
import net.minecraft.sounds.SoundSource;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.level.Level;

/** Client-side particle and sound effects. Nothing here is sent to the server. */
public final class HitEffects {
	private HitEffects() {
	}

	private static final ParticleOptions FALLBACK = lookupParticle("minecraft:crit").orElse(null);

	public static void onHit(Level level, Entity target, FxConfig cfg) {
		ParticleOptions particle = lookupParticle(cfg.hitParticle).orElse(FALLBACK);
		if (particle != null) {
			switch (cfg.hitShape.toUpperCase(Locale.ROOT)) {
				case "RING" -> ring(level, target, particle, cfg.particleCount, cfg.particleSpeed);
				case "SPIRAL" -> spiral(level, target, particle, cfg.particleCount, cfg.particleSpeed);
				default -> burst(level, target, particle, cfg.particleCount, cfg.particleSpeed);
			}
		}
		playSound(level, target, cfg.hitSound, cfg.soundVolume, cfg.soundPitch);
	}

	public static void onKill(Level level, Entity target, FxConfig cfg) {
		switch (cfg.killEffect.toUpperCase(Locale.ROOT)) {
			case "PILLAR" -> pillar(level, target, "minecraft:end_rod");
			case "SOULS" -> pillar(level, target, "minecraft:soul_fire_flame");
			case "EXPLOSION" -> {
				lookupParticle("minecraft:explosion").ifPresent(p ->
						level.addParticle(p, target.getX(), target.getY() + target.getBbHeight() / 2, target.getZ(), 0, 0, 0));
				lookupParticle("minecraft:flame").ifPresent(p -> burst(level, target, p, 40, 1.6f));
			}
			case "TOTEM" -> lookupParticle("minecraft:totem_of_undying").ifPresent(p -> burst(level, target, p, 60, 2.2f));
			default -> {
				return;
			}
		}
		playSound(level, target, cfg.killSound, cfg.soundVolume, 1.0f);
	}

	// --- shapes -------------------------------------------------------------

	private static void burst(Level level, Entity t, ParticleOptions p, int count, float speed) {
		ThreadLocalRandom r = ThreadLocalRandom.current();
		double cx = t.getX();
		double cy = t.getY() + t.getBbHeight() * 0.6;
		double cz = t.getZ();
		double spread = Math.max(0.3, t.getBbWidth() * 0.5);
		for (int i = 0; i < count; i++) {
			// random direction on a sphere
			double theta = r.nextDouble(Math.PI * 2);
			double phi = Math.acos(2 * r.nextDouble() - 1);
			double dx = Math.sin(phi) * Math.cos(theta);
			double dy = Math.cos(phi);
			double dz = Math.sin(phi) * Math.sin(theta);
			double v = (0.12 + r.nextDouble() * 0.22) * speed;
			level.addParticle(p,
					cx + dx * spread, cy + dy * spread * 0.8, cz + dz * spread,
					dx * v, dy * v + 0.05, dz * v);
		}
	}

	private static void ring(Level level, Entity t, ParticleOptions p, int count, float speed) {
		double cy = t.getY() + t.getBbHeight() * 0.5;
		double radius = t.getBbWidth() * 0.6 + 0.35;
		for (int i = 0; i < count; i++) {
			double a = Math.PI * 2 * i / count;
			double dx = Math.cos(a);
			double dz = Math.sin(a);
			double v = 0.08 * speed;
			level.addParticle(p, t.getX() + dx * radius, cy, t.getZ() + dz * radius, dx * v, 0.01, dz * v);
		}
	}

	private static void spiral(Level level, Entity t, ParticleOptions p, int count, float speed) {
		double radius = t.getBbWidth() * 0.55 + 0.25;
		double h = t.getBbHeight();
		for (int i = 0; i < count; i++) {
			double k = (double) i / Math.max(1, count - 1);
			double a = k * Math.PI * 4;
			level.addParticle(p,
					t.getX() + Math.cos(a) * radius, t.getY() + k * h, t.getZ() + Math.sin(a) * radius,
					0, 0.03 * speed, 0);
		}
	}

	private static void pillar(Level level, Entity t, String particleId) {
		lookupParticle(particleId).ifPresent(p -> {
			ThreadLocalRandom r = ThreadLocalRandom.current();
			for (int i = 0; i < 40; i++) {
				level.addParticle(p,
						t.getX() + (r.nextDouble() - 0.5) * 0.4,
						t.getY() + r.nextDouble() * 0.5,
						t.getZ() + (r.nextDouble() - 0.5) * 0.4,
						(r.nextDouble() - 0.5) * 0.05, 0.25 + r.nextDouble() * 0.35, (r.nextDouble() - 0.5) * 0.05);
			}
		});
	}

	// --- registry lookups ---------------------------------------------------

	/**
	 * Looks a particle up by id. Only parameterless particle types can be used this way
	 * (they implement ParticleOptions themselves); anything else returns empty.
	 */
	static Optional<ParticleOptions> lookupParticle(String id) {
		Identifier key = Identifier.tryParse(id == null ? "" : id.trim());
		if (key == null) return Optional.empty();
		Optional<ParticleType<?>> type = BuiltInRegistries.PARTICLE_TYPE.getOptional(key);
		return type.filter(ParticleOptions.class::isInstance).map(ParticleOptions.class::cast);
	}

	private static void playSound(Level level, Entity t, String id, float volume, float pitch) {
		if (id == null || id.isBlank() || id.equalsIgnoreCase("none") || volume <= 0) return;
		Identifier key = Identifier.tryParse(id.trim());
		if (key == null) return;
		Optional<SoundEvent> sound = BuiltInRegistries.SOUND_EVENT.getOptional(key);
		sound.ifPresent(s -> level.playLocalSound(t.getX(), t.getY(), t.getZ(), s, SoundSource.PLAYERS, volume, pitch, false));
	}
}
