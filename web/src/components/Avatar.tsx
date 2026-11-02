import type { ComponentChildren } from 'preact';
import { Dodec } from './Dodec';
import * as api from '../api';

// Avatar is a persona's profile picture: a rounded square with a hue-derived
// gradient and the persona's emoji centered. The default persona shows the
// Dodec brand mark instead of an emoji (paper background, seal strokes) so
// the "no persona selected" identity is visually distinct from real ones.
//
// Uploaded pictures (this backend's extension over the original emoji-only
// avatars) take precedence when present: personaAvatar renders the image
// instead of the gradient whenever hasAvatar is set.

interface Props {
  emoji: string;
  hue: number;
  size?: number;
  // glyph overrides the emoji (used for the default persona's dodec mark).
  glyph?: ComponentChildren;
  // ring adds a subtle outline for use on tinted backgrounds.
  ring?: boolean;
}

export function Avatar({ emoji, hue, size = 28, glyph, ring = true }: Props) {
  const style = glyph
    ? {
        width: size,
        height: size,
        background: 'var(--paper-card)',
        boxShadow: 'inset 0 0 0 1px rgb(var(--hairline))',
      }
    : {
        width: size,
        height: size,
        background: `linear-gradient(135deg, hsl(${hue} 44% 62%), hsl(${(hue + 38) % 360} 40% 44%))`,
      };

  return (
    <span
      class={`persona-avatar inline-flex items-center justify-center rounded-lg flex-shrink-0 select-none shadow-sm ${ring && !glyph ? 'ring-1 ring-black/5' : ''}`}
      style={style}
      title={emoji}
    >
      {glyph ?? (
        <span style={{ fontSize: Math.round(size * 0.52), lineHeight: 1 }}>{emoji}</span>
      )}
    </span>
  );
}

// ImageAvatar renders an uploaded profile picture as a rounded square.
export function ImageAvatar({ url, name, size = 28 }: { url: string; name: string; size?: number }) {
  return (
    <img
      src={url}
      alt={name}
      width={size}
      height={size}
      class="persona-avatar inline-flex items-center justify-center rounded-lg flex-shrink-0 select-none object-cover shadow-sm ring-1 ring-black/5"
      style={{ width: size, height: size }}
    />
  );
}

// DefaultAvatar renders the built-in persona's avatar (mini Dodec on paper).
export function DefaultAvatar({ size = 28 }: { size?: number }) {
  return (
    <Avatar
      emoji="a1"
      hue={200}
      size={size}
      glyph={<Dodec size={Math.round(size * 0.68)} animate={false} />}
    />
  );
}

// personaAvatar picks the right avatar for a persona record: an uploaded
// picture wins, the default persona gets the brand mark, everyone else
// gets emoji-on-gradient.
export function personaAvatar(p: api.Persona | { id: string; emoji: string; hue: number; hasAvatar?: boolean; avatarVersion?: number; name?: string }, size = 28) {
  const url = api.personaAvatarURL({ id: p.id, hasAvatar: p.hasAvatar, avatarVersion: p.avatarVersion });
  if (url) {
    return <ImageAvatar url={url} name={p.name ?? p.id} size={size} />;
  }
  if (p.id === 'default') {
    return <DefaultAvatar size={size} />;
  }
  return <Avatar emoji={p.emoji} hue={p.hue} size={size} />;
}
