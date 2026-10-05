"use client";

import { useEffect, useState } from "react";
import type { z } from "zod";

import { mintOperationAuthority } from "@/features/orgs/actions";
import {
  callControl,
  type ControlOperation,
  type ControlResult,
} from "@/lib/operation-authority";

/** A control read in flight, or what it answered. */
export type ControlRead<T> = { status: "loading" } | ControlResult<T>;

/** Where a read goes: the hosted operator and the route under it. */
export type ControlReadTarget = {
  endpoint: string;
  audience: string;
  path: string;
  target: string;
};

/**
 * Reads one control route under a fresh authority whenever the route or
 * `generation` changes, and answers null where `where` is null. Each
 * attempt mints its own request identifier, so a re-read is a new
 * operation rather than a replay.
 */
export function useControlRead<T>(
  org: string,
  operation: ControlOperation,
  where: ControlReadTarget | null,
  parser: z.ZodType<T>,
  generation = 0,
): ControlRead<T> | null {
  const endpoint = where?.endpoint ?? null;
  const audience = where?.audience ?? "";
  const path = where?.path ?? "";
  const target = where?.target ?? "";
  const key = endpoint === null ? null : `${generation} ${endpoint}${path}`;
  const [answer, setAnswer] = useState<{
    key: string;
    read: ControlResult<T>;
  } | null>(null);

  useEffect(() => {
    if (endpoint === null || key === null) return;
    let live = true;
    void callControl(
      {
        endpoint,
        path,
        method: "GET",
        requestId: crypto.randomUUID(),
        org,
        audience,
        operation,
        target,
        parser,
      },
      mintOperationAuthority,
    ).then((read) => {
      if (live) setAnswer({ key, read });
    });
    return () => {
      live = false;
    };
  }, [key, endpoint, path, org, audience, operation, target, parser]);

  if (key === null) return null;
  return answer?.key === key ? answer.read : { status: "loading" };
}
