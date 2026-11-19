// CtaButton — the main call to action (renew, connect a tariff): a solid
// brand-accent button, the one loud element on a quiet screen.
import { Button, type ButtonProps } from '@mantine/core'
import type { ReactNode } from 'react'

import classes from './cta-button.module.css'

export function CtaButton({ children, onClick, ...props }: ButtonProps & { children: ReactNode; onClick?: () => void }) {
    return (
        <Button
            className={classes.cta}
            onClick={onClick}
            radius="md"
            variant="filled"
            {...props}
        >
            {children}
        </Button>
    )
}
