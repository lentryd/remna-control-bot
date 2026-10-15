// Copied from remna-control/web/src/shared/constants/theme/components.ts
import {
    Accordion,
    ActionIcon,
    Alert,
    Anchor,
    Autocomplete,
    Badge,
    Button,
    Card,
    Checkbox,
    Code,
    Combobox,
    Divider,
    Drawer,
    Fieldset,
    Input,
    InputWrapper,
    Kbd,
    Loader,
    Menu,
    Modal,
    MultiSelect,
    Paper,
    PasswordInput,
    Popover,
    Progress,
    RingProgress,
    ScrollArea,
    SegmentedControl,
    Select,
    Skeleton,
    Switch,
    Table,
    Tabs,
    Textarea,
    TextInput,
    ThemeIcon,
    Tooltip,
    Notification
} from '@mantine/core'

import classes from './components.module.css'

// Some Android browsers report a fine pointer, so hover and width count too.
const touch = typeof window !== 'undefined' && window.matchMedia('(pointer: coarse), (hover: none), (max-width: 48em)').matches

// Descriptions go under the input so fields side by side in a grid stay
// aligned when only one of them has a hint.
const field: { radius: 'md'; inputWrapperOrder: ('label' | 'input' | 'description' | 'error')[] } = {
    radius: 'md',
    inputWrapperOrder: ['label', 'input', 'description', 'error']
}

const fieldClassNames = {
    input: classes.input,
    label: classes.inputLabel,
    description: classes.inputDescription
}

const comboboxClassNames = { ...fieldClassNames, dropdown: classes.dropdown, option: classes.option }

export const components = {
    Card: Card.extend({
        classNames: { root: classes.card },
        defaultProps: { radius: 'lg', withBorder: true, padding: 'lg' }
    }),
    Paper: Paper.extend({
        classNames: { root: classes.paper },
        defaultProps: { radius: 'lg' }
    }),
    Fieldset: Fieldset.extend({
        classNames: { root: classes.fieldset, legend: classes.fieldsetLegend }
    }),
    Button: Button.extend({
        classNames: { root: classes.button },
        defaultProps: { radius: 'md', variant: 'default' }
    }),
    ActionIcon: ActionIcon.extend({
        classNames: { root: classes.actionIcon },
        defaultProps: { radius: 'md', variant: 'subtle', color: 'gray' }
    }),
    Badge: Badge.extend({
        classNames: { root: classes.badge, label: classes.badgeLabel },
        defaultProps: { radius: 'sm', variant: 'soft' }
    }),
    ThemeIcon: ThemeIcon.extend({
        defaultProps: { radius: 'md', variant: 'soft' }
    }),
    Anchor: Anchor.extend({
        defaultProps: { underline: 'hover' }
    }),
    InputWrapper: InputWrapper.extend({
        classNames: { label: classes.inputLabel, description: classes.inputDescription }
    }),
    Input: Input.extend({
        classNames: { input: classes.input }
    }),
    TextInput: TextInput.extend({ defaultProps: field, classNames: fieldClassNames }),
    PasswordInput: PasswordInput.extend({ defaultProps: field, classNames: fieldClassNames }),
    Textarea: Textarea.extend({ defaultProps: field, classNames: fieldClassNames }),
    // Plain objects instead of X.extend() for components the app shell doesn't
    // render, so they (and react-number-format, @mantine/dates) load with the
    // pages that use them.
    // Separators follow the UI language (LocaleProvider).
    NumberInput: {
        defaultProps: field,
        classNames: fieldClassNames
    },
    Select: Select.extend({
        defaultProps: { ...field, allowDeselect: false, checkIconPosition: 'right' },
        classNames: comboboxClassNames
    }),
    MultiSelect: MultiSelect.extend({ defaultProps: field, classNames: comboboxClassNames }),
    Autocomplete: Autocomplete.extend({ defaultProps: field, classNames: comboboxClassNames }),
    DateInput: { defaultProps: field, classNames: fieldClassNames },
    DatePickerInput: { defaultProps: field, classNames: fieldClassNames },
    MonthPickerInput: { defaultProps: field, classNames: fieldClassNames },
    DateTimePicker: { defaultProps: field, classNames: fieldClassNames },
    Combobox: Combobox.extend({
        classNames: { dropdown: classes.dropdown, option: classes.option },
        defaultProps: {
            shadow: 'lg',
            radius: 'md',
            transitionProps: { transition: 'pop', duration: 140 },
            // On phones the on-screen keyboard resizes the viewport while
            // the dropdown is open: flipping above/below and hiding when
            // the input is briefly "detached" made it flicker.
            ...(touch && { position: 'bottom', middlewares: { flip: false, shift: false }, hideDetached: false })
        }
    }),
    Popover: Popover.extend({
        classNames: { dropdown: classes.dropdown },
        defaultProps: { radius: 'md', shadow: 'lg', transitionProps: { transition: 'pop', duration: 140 } }
    }),
    Menu: Menu.extend({
        classNames: {
            dropdown: classes.dropdown,
            item: classes.menuItem,
            label: classes.menuLabel,
            divider: classes.menuDivider
        },
        defaultProps: {
            radius: 'md',
            shadow: 'lg',
            withArrow: false,
            transitionProps: { transition: 'pop-top-right', duration: 140 }
        }
    }),
    Tooltip: Tooltip.extend({
        classNames: { tooltip: classes.tooltip },
        defaultProps: {
            radius: 'sm',
            withArrow: false,
            openDelay: 150,
            transitionProps: { transition: 'fade', duration: 120 }
        }
    }),
    Modal: Modal.extend({
        classNames: {
            root: classes.modalRoot,
            content: classes.modalContent,
            header: classes.modalHeader,
            title: classes.modalTitle,
            body: classes.modalBody
        },
        defaultProps: {
            radius: 'lg',
            centered: true,
            overlayProps: { backgroundOpacity: 1, color: 'var(--app-overlay)', blur: 3 },
            transitionProps: { transition: 'pop', duration: 180 }
        }
    }),
    Drawer: Drawer.extend({
        classNames: { content: classes.drawerContent, header: classes.drawerHeader, body: classes.drawerBody },
        defaultProps: {
            overlayProps: { backgroundOpacity: 1, color: 'var(--app-overlay)', blur: 3 }
        }
    }),
    SegmentedControl: SegmentedControl.extend({
        classNames: { root: classes.segmentedRoot, indicator: classes.segmentedIndicator, label: classes.segmentedLabel },
        defaultProps: { radius: 'md', transitionDuration: 160 }
    }),
    Tabs: Tabs.extend({
        classNames: { list: classes.tabsList, tab: classes.tab },
        defaultProps: { variant: 'default' }
    }),
    Table: Table.extend({
        classNames: { table: classes.table, th: classes.th },
        defaultProps: { highlightOnHover: true, verticalSpacing: 'sm' }
    }),
    Notification: Notification.extend({
        classNames: { root: classes.notification, title: classes.notificationTitle, description: classes.notificationDescription },
        defaultProps: { radius: 'lg' }
    }),
    Alert: Alert.extend({
        classNames: { root: classes.alert, title: classes.alertTitle },
        defaultProps: { radius: 'lg', variant: 'light' }
    }),
    Progress: Progress.extend({
        classNames: { root: classes.progressRoot },
        defaultProps: { radius: 'xl' }
    }),
    RingProgress: RingProgress.extend({
        defaultProps: { thickness: 8, roundCaps: true, rootColor: 'var(--app-fill-hover)' }
    }),
    Skeleton: Skeleton.extend({
        classNames: { root: classes.skeleton },
        defaultProps: { radius: 'md' }
    }),
    Switch: Switch.extend({
        defaultProps: { radius: 'xl' }
    }),
    Checkbox: Checkbox.extend({
        defaultProps: { radius: 'sm' }
    }),
    Kbd: Kbd.extend({
        classNames: { root: classes.kbd }
    }),
    Code: Code.extend({
        classNames: { root: classes.code }
    }),
    Divider: Divider.extend({
        classNames: { root: classes.divider }
    }),
    Accordion: Accordion.extend({
        classNames: { item: classes.accordionItem, control: classes.accordionControl }
    }),
    Loader: Loader.extend({
        defaultProps: { type: 'oval' }
    }),
    ScrollArea: ScrollArea.extend({
        defaultProps: { scrollbarSize: 6 }
    }),
    // A plain object instead of BarChart.extend(): importing the component here
    // would pull recharts into the main bundle instead of the pages' chunks.
    BarChart: {
        defaultProps: { barProps: { radius: [6, 6, 2, 2] } }
    }
}
