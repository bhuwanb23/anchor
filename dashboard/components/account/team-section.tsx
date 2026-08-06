"use client";

import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import api from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface Team {
  id: string;
  name: string;
  owner_id: string;
}

interface Member {
  id: string;
  user_id: string;
  role: string;
  email?: string;
  name?: string;
}

export function TeamSection() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [teamId, setTeamId] = useState<string>("");
  const [members, setMembers] = useState<Member[]>([]);
  const [email, setEmail] = useState("");
  const [inviteLink, setInviteLink] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const res = await api.get<Team[] | { teams?: Team[] }>("/api/v1/teams");
      const list = Array.isArray(res.data) ? res.data : res.data.teams || [];
      setTeams(list);
      if (list.length && !teamId) setTeamId(list[0].id);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, [teamId]);

  const loadMembers = useCallback(async (id: string) => {
    if (!id) return;
    try {
      const res = await api.get<Member[] | { members?: Member[] }>(
        `/api/v1/teams/${id}/members`
      );
      const list = Array.isArray(res.data) ? res.data : res.data.members || [];
      setMembers(list);
    } catch {
      setMembers([]);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (teamId) loadMembers(teamId);
  }, [teamId, loadMembers]);

  const invite = async () => {
    if (!teamId || !email.trim()) return;
    try {
      const res = await api.post<{
        token?: string;
        accept_path?: string;
        email: string;
      }>(`/api/v1/teams/${teamId}/invite`, {
        email: email.trim(),
        role: "member",
      });
      toast.success(`Invitation sent to ${res.data.email}`);
      if (res.data.token) {
        const path = res.data.accept_path || `/invite/${res.data.token}`;
        const link = `${window.location.origin}${path}`;
        setInviteLink(link);
      }
      setEmail("");
    } catch (e) {
      const msg =
        (e as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        (e instanceof Error ? e.message : "Invite failed");
      toast.error(msg);
    }
  };

  if (loading) {
    return <div className="h-24 animate-pulse rounded-xl bg-gray-200 dark:bg-gray-800" />;
  }

  if (teams.length === 0) {
    return (
      <p className="text-sm text-gray-500">No teams yet — one is created when you sign up.</p>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Team</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {teams.length > 1 && (
          <select
            value={teamId}
            onChange={(e) => setTeamId(e.target.value)}
            className="w-full rounded-md border px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-950"
          >
            {teams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        )}

        <div>
          <p className="mb-2 text-xs font-medium text-gray-500">Members</p>
          {members.length === 0 ? (
            <p className="text-sm text-gray-500">Only you so far.</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {members.map((m) => (
                <li key={m.id} className="flex justify-between">
                  <span>{m.email || m.name || m.user_id.slice(0, 8)}</span>
                  <span className="text-gray-400 capitalize">{m.role}</span>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="space-y-2">
          <p className="text-xs font-medium text-gray-500">Invite a teammate</p>
          <div className="flex gap-2">
            <Input
              type="email"
              placeholder="colleague@email.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
            <Button onClick={invite} disabled={!email.trim()}>
              Invite
            </Button>
          </div>
          {inviteLink && (
            <div className="rounded-md bg-gray-50 p-3 text-xs dark:bg-gray-900">
              <p className="mb-1 text-gray-500">Share this accept link (email may not be configured locally):</p>
              <button
                type="button"
                className="break-all text-left text-blue-600 hover:underline"
                onClick={() => {
                  navigator.clipboard.writeText(inviteLink);
                  toast.success("Invite link copied");
                }}
              >
                {inviteLink}
              </button>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
