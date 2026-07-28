/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, useState } from 'react'
import { Check, ChevronsUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

type BillingGroupComboboxProps = {
  id?: string
  groups: string[]
  value: string
  disabled?: boolean
  onValueChange: (value: string) => void
}

export function BillingGroupCombobox(props: BillingGroupComboboxProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const options = useMemo(() => {
    const normalizedSearch = search.trim().toLocaleLowerCase()
    const sortedGroups = [...new Set(props.groups.filter(Boolean))].sort(
      (left, right) => left.localeCompare(right)
    )
    if (!normalizedSearch) return sortedGroups
    return sortedGroups.filter((group) =>
      group.toLocaleLowerCase().includes(normalizedSearch)
    )
  }, [props.groups, search])

  const select = (value: string) => {
    props.onValueChange(value)
    setOpen(false)
    setSearch('')
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            id={props.id}
            variant='outline'
            role='combobox'
            aria-expanded={open}
            disabled={props.disabled}
            className='h-9 w-full justify-between bg-transparent px-3 font-normal'
          />
        }
      >
        <span
          className={cn('truncate', !props.value && 'text-muted-foreground')}
        >
          {props.value || t('Billing all groups')}
        </span>
        <ChevronsUpDown className='size-4 shrink-0 opacity-50' />
      </PopoverTrigger>
      <PopoverContent
        className='w-[var(--anchor-width)] overflow-hidden p-0'
        onWheel={(event) => event.stopPropagation()}
      >
        <Command shouldFilter={false}>
          <CommandInput
            value={search}
            placeholder={t('Billing search groups')}
            onValueChange={setSearch}
          />
          <CommandList className='max-h-72'>
            <CommandEmpty>{t('Billing no groups found')}</CommandEmpty>
            <CommandGroup>
              {!search.trim() && (
                <CommandItem value='__all__' onSelect={() => select('')}>
                  <Check
                    className={cn(
                      'size-4',
                      props.value ? 'opacity-0' : 'opacity-100'
                    )}
                  />
                  {t('Billing all groups')}
                </CommandItem>
              )}
              {options.map((group) => (
                <CommandItem
                  key={group}
                  value={group}
                  onSelect={() => select(group)}
                >
                  <Check
                    className={cn(
                      'size-4',
                      props.value === group ? 'opacity-100' : 'opacity-0'
                    )}
                  />
                  <span className='truncate'>{group}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
