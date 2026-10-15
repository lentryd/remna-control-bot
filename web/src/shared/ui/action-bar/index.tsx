// Adapted from remna-control/web/src/pages/SettingsPage.tsx (the sticky save bar)
import { Text, Transition } from '@mantine/core'
import type { ReactNode } from 'react'

import classes from './action-bar.module.css'

const transition = {
    in: { opacity: 1, transform: 'translateY(0)' },
    out: { opacity: 0, transform: 'translateY(12px)' },
    transitionProperty: 'opacity, transform'
}

// ActionBar floats at the bottom of the screen while `mounted`: what it is
// about (title and a message, coloured by `tone`) and the buttons. Keep one
// per screen so bars never overlap.
export function ActionBar({ mounted, title, message, tone, children }: { mounted: boolean; title: ReactNode; message?: ReactNode; tone?: 'ok' | 'error'; children: ReactNode }) {
    return (
        <Transition duration={160} mounted={mounted} transition={transition}>
            {(style) => (
                <div aria-live="polite" className={classes.bar} role="status" style={style}>
                    <div className={classes.text}>
                        <Text fw={600} size="sm" truncate>
                            {title}
                        </Text>
                        {message && (
                            <Text c={tone === 'error' ? 'red' : tone === 'ok' ? 'teal' : 'dimmed'} lh={1.35} mt={2} size="xs">
                                {message}
                            </Text>
                        )}
                    </div>
                    <div className={classes.buttons}>{children}</div>
                </div>
            )}
        </Transition>
    )
}
