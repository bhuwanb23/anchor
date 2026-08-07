"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { StatusBadge } from "@/components/ui/badge";
import api from "@/lib/api";
import { toast } from "sonner";
import { Loader2, Plus, Trash2 } from "lucide-react";

interface DomainRow {
  id: string;
  domain: string;
  status: string;
}

interface DomainsSectionProps {
  serverId: string;
  appId: string;
  serverIp?: string;
}

export function DomainsSection({ serverId, appId, serverIp }: DomainsSectionProps) {
  const [domains, setDomains] = useState<DomainRow[]>([]);
  const [open, setOpen] = useState(false);
  const [domain, setDomain] = useState("");
  const [dnsInfo, setDnsInfo] = useState<{
    type: string;
    name: string;
    value: string;
    ttl: string;
    domainId: string;
  } | null>(null);
  const [verifying, setVerifying] = useState(false);
  const [verifyMsg, setVerifyMsg] = useState("");
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const startedAt = useRef(0);

  const load = useCallback(async () => {
    try {
      const res = await api.get<{ domains: DomainRow[] }>(
        `/api/v1/servers/${serverId}/apps/${appId}/domains`
      );
      setDomains(res.data.domains || []);
    } catch {
      setDomains([]);
    }
  }, [serverId, appId]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, []);

  const startVerifyPoll = (domainId: string, domainName: string) => {
    setVerifying(true);
    setVerifyMsg("Checking DNS…");
    startedAt.current = Date.now();
    if (pollRef.current) clearInterval(pollRef.current);

    const tick = async () => {
      try {
        const res = await api.post<{
          verified?: boolean;
          message?: string;
          expected_ip?: string;
        }>(`/api/v1/servers/${serverId}/apps/${appId}/domains/${domainId}/verify`);
        if (res.data.verified) {
          setVerifying(false);
          setVerifyMsg("Domain verified and active ✓");
          if (pollRef.current) clearInterval(pollRef.current);
          load();
          return;
        }
        const elapsed = Date.now() - startedAt.current;
        if (elapsed > 120_000) {
          setVerifyMsg("Still waiting for DNS… Propagation can take a few minutes to 48 hours.");
        } else {
          setVerifyMsg(res.data.message || "Waiting for DNS propagation…");
        }
      } catch {
        setVerifyMsg(`Still waiting for DNS for ${domainName}…`);
      }
    };

    tick();
    pollRef.current = setInterval(tick, 10_000);
  };

  const addDomain = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const res = await api.post<{
        id: string;
        domain: string;
        server_ip: string;
        dns_instructions: { type: string; name: string; value: string; ttl: string };
      }>(`/api/v1/servers/${serverId}/apps/${appId}/domains`, { domain });
      const ip = res.data.server_ip || serverIp || "your-server-ip";
      const host = domain.split(".")[0] || domain;
      setDnsInfo({
        type: "A",
        name: host,
        value: ip,
        ttl: "3600",
        domainId: res.data.id,
      });
      toast.success("Domain added");
      load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to add domain");
    }
  };

  const remove = async (d: DomainRow) => {
    try {
      await api.delete(`/api/v1/servers/${serverId}/apps/${appId}/domains/${d.id}`);
      toast.success("Domain removed");
      load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to remove");
    }
  };

  return (
    <div className="space-y-3">
      <div className="divide-y rounded-lg border border-gray-200 dark:divide-gray-800 dark:border-gray-800">
        {domains.length === 0 && (
          <p className="px-4 py-3 text-sm text-gray-500">No custom domains configured.</p>
        )}
        {domains.map((d) => (
          <div key={d.id} className="flex items-center gap-3 px-4 py-2.5">
            <span className="flex-1 text-sm font-medium">{d.domain}</span>
            <StatusBadge status={d.status === "verified" || d.status === "active" ? "running" : "pending"} />
            <Button size="sm" variant="ghost" onClick={() => remove(d)}>
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
      </div>
      <Button size="sm" variant="secondary" onClick={() => { setOpen(true); setDnsInfo(null); setDomain(""); setVerifyMsg(""); }}>
        <Plus className="mr-1 h-3.5 w-3.5" />
        Add Custom Domain
      </Button>

      <Dialog
        open={open}
        onOpenChange={(o) => {
          setOpen(o);
          if (!o && pollRef.current) clearInterval(pollRef.current);
        }}
      >
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Add Custom Domain</DialogTitle>
          </DialogHeader>
          {!dnsInfo ? (
            <form onSubmit={addDomain} className="space-y-4">
              <Input
                label="Domain name"
                value={domain}
                onChange={(e) => setDomain(e.target.value)}
                placeholder="shop.yourdomain.com"
                required
              />
              <DialogFooter>
                <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
                  Cancel
                </Button>
                <Button type="submit">Continue</Button>
              </DialogFooter>
            </form>
          ) : (
            <div className="space-y-4">
              <p className="text-sm text-gray-700 dark:text-gray-200">
                Point <strong>{domain}</strong> to <strong>{dnsInfo.value}</strong> (your server IP)
              </p>
              <div className="rounded-lg bg-[var(--color-paper-2)] p-3 font-mono text-xs dark:bg-gray-800">
                <div>Type: {dnsInfo.type}</div>
                <div>Name: {dnsInfo.name}</div>
                <div>Value: {dnsInfo.value}</div>
                <div>TTL: {dnsInfo.ttl}</div>
              </div>
              {verifying && (
                <p className="flex items-center gap-2 text-sm text-gray-600">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {verifyMsg}
                </p>
              )}
              {!verifying && verifyMsg && (
                <p className="text-sm text-green-700 dark:text-green-400">{verifyMsg}</p>
              )}
              <DialogFooter>
                {!verifying && !verifyMsg.includes("verified") && (
                  <Button onClick={() => startVerifyPoll(dnsInfo.domainId, domain)}>
                    I&apos;ve made the DNS change
                  </Button>
                )}
                <Button variant="secondary" onClick={() => setOpen(false)}>
                  Done
                </Button>
              </DialogFooter>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
