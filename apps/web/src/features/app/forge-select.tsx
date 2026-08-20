"use client";

import { KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { translate as t } from "@forgeflow/ui";

export type SelectOption = { value: string; label: string };

function uniqueOptions(options: SelectOption[]) {
  const seen = new Set<string>();
  return options.filter((option) => !seen.has(option.value) && seen.add(option.value));
}

export function ForgeSelect({ ariaLabel, value, options, placeholder, searchable = false, disabled = false, onChange }: { ariaLabel: string; value: string; options: SelectOption[]; placeholder: string; searchable?: boolean; disabled?: boolean; onChange: (value: string) => void }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listboxID = `forge-select-${useId().replaceAll(":", "")}`;
  const normalizedOptions = uniqueOptions(options);
  const filteredOptions = normalizedOptions.filter((option) => option.label.toLowerCase().includes(query.trim().toLowerCase()));
  const selectedOption = normalizedOptions.find((option) => option.value === value);
  const activeOption = filteredOptions[activeIndex];

  useEffect(() => {
    if (open && searchable && normalizedOptions.length) queueMicrotask(() => searchRef.current?.focus());
  }, [normalizedOptions.length, open, searchable]);

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false);
        setQuery("");
      }
    }
    document.addEventListener("mousedown", closeOnOutsideClick);
    return () => document.removeEventListener("mousedown", closeOnOutsideClick);
  }, [open]);

  function choose(option?: SelectOption) {
    if (!option) return;
    onChange(option.value);
    setOpen(false);
    setQuery("");
  }

  function moveActive(direction: 1 | -1) {
    if (!filteredOptions.length) return;
    setActiveIndex((current) => (current + direction + filteredOptions.length) % filteredOptions.length);
  }

  function handleKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key === " " && event.currentTarget instanceof HTMLInputElement) return;
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
      setQuery("");
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
      moveActive(1);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setOpen(true);
      moveActive(-1);
    } else if (event.key === "Home") {
      event.preventDefault();
      setActiveIndex(0);
    } else if (event.key === "End") {
      event.preventDefault();
      setActiveIndex(Math.max(filteredOptions.length - 1, 0));
    } else if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      if (open) choose(activeOption ?? filteredOptions[0]);
      else setOpen(true);
    }
  }

  return <div className="app-v2-select" ref={rootRef}><button className={`app-v2-select-trigger${open ? " is-open" : ""}`} type="button" role="combobox" aria-label={ariaLabel} aria-haspopup="listbox" aria-expanded={open} aria-controls={listboxID} aria-activedescendant={open && activeOption ? `${listboxID}-option-${activeIndex}` : undefined} disabled={disabled} onClick={() => { setOpen((current) => !current); setQuery(""); }} onKeyDown={handleKeyDown}><span className={selectedOption ? "" : "is-placeholder"}>{selectedOption?.label ?? placeholder}</span><span className="app-v2-select-chevron" aria-hidden="true">⌄</span></button>{open ? <div className="app-v2-select-panel">{searchable && normalizedOptions.length ? <div className="app-v2-select-search"><span aria-hidden="true">⌕</span><input ref={searchRef} value={query} onChange={(event) => { setQuery(event.target.value); setActiveIndex(0); }} onKeyDown={handleKeyDown} placeholder={t("select.search", { label: ariaLabel.toLowerCase() })} aria-label={t("select.search", { label: ariaLabel })} autoComplete="off" /></div> : null}<div className="app-v2-select-options" id={listboxID} role="listbox" aria-label={ariaLabel}>{filteredOptions.length ? filteredOptions.map((option, index) => <button className={`app-v2-select-option${option.value === value ? " is-selected" : ""}${index === activeIndex ? " is-active" : ""}`} id={`${listboxID}-option-${index}`} key={`${option.value}-${option.label}`} type="button" role="option" aria-selected={option.value === value} onMouseEnter={() => setActiveIndex(index)} onClick={() => choose(option)}><span>{option.label}</span>{option.value === value ? <span aria-hidden="true">✓</span> : null}</button>) : <span className="app-v2-select-empty">{t("select.empty")}</span>}</div></div> : null}</div>;
}
